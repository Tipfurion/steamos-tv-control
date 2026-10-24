package tv

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"

	pb "github.com/jkiddo/atvremote/pkg/v2/proto"
	atv "github.com/jkiddo/atvremote/pkg/v2/remote"
	"google.golang.org/protobuf/proto"
)

// A control session owns its socket and observes EOF directly. The upstream
// pairing client does not expose control-connection liveness; its cached IsOn
// value remains available after disconnect. Keeping this small control session
// lets sleep reuse a ready socket without treating an old report as live state.
type remoteSession struct {
	conn      net.Conn
	writes    chan struct{}
	ready     chan struct{}
	done      chan struct{}
	readyOnce sync.Once
	closeOnce sync.Once
	power     atomic.Int32
	mu        sync.RWMutex
	info      *atv.DeviceInfo
	err       error
	features  int32
	onPower   func(bool)
}

func newSession(conn net.Conn, onPower func(bool)) *remoteSession {
	return &remoteSession{conn: conn, writes: make(chan struct{}, 1), ready: make(chan struct{}), done: make(chan struct{}), onPower: onPower,
		features: int32(atv.FeaturePing | atv.FeatureKey | atv.FeaturePower | atv.FeatureVolume | atv.FeatureAppLink)}
}

func (s *remoteSession) alive() bool {
	select {
	case <-s.done:
		return false
	default:
		return true
	}
}

func (s *remoteSession) Power() string {
	if !s.alive() {
		return "unknown"
	}
	switch s.power.Load() {
	case 1:
		return "on"
	case 2:
		return "standby"
	default:
		return "unknown"
	}
}

func (s *remoteSession) Info() *atv.DeviceInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.info == nil || !s.alive() {
		return nil
	}
	info := *s.info
	return &info
}

func (s *remoteSession) close(err error) {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.err = err
		s.mu.Unlock()
		close(s.done)
		// Do not leave an unacknowledged POWER queued in the kernel after
		// suspend. A graceful TCP close could deliver it when Wi-Fi returns,
		// reversing the new resume action several seconds later.
		raw := s.conn
		if secured, ok := raw.(*tls.Conn); ok {
			raw = secured.NetConn()
		}
		if tcp, ok := raw.(*net.TCPConn); ok {
			_ = tcp.SetLinger(0)
		}
		raw.Close()
		slog.Debug("TV control connection closed", "error", err)
	})
}

func (s *remoteSession) failure() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return &atv.ConnectionClosed{Message: "TV control connection closed", Err: s.err}
}

func (s *remoteSession) send(ctx context.Context, msg *pb.RemoteMessage) error {
	select {
	case s.writes <- struct{}{}:
		defer func() { <-s.writes }()
	case <-ctx.Done():
		return ctx.Err()
	case <-s.done:
		return s.failure()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !s.alive() {
		return s.failure()
	}
	b, err := proto.Marshal(msg)
	if err != nil {
		return err
	}
	frame := binary.AppendUvarint(nil, uint64(len(b)))
	frame = append(frame, b...)
	deadline := time.Now().Add(3 * time.Second)
	if end, ok := ctx.Deadline(); ok && end.Before(deadline) {
		deadline = end
	}
	if err = s.conn.SetWriteDeadline(deadline); err != nil {
		s.close(err)
		return err
	}
	stop := context.AfterFunc(ctx, func() { s.close(ctx.Err()) })
	defer stop()
	for len(frame) > 0 {
		n, err := s.conn.Write(frame)
		if err != nil {
			s.close(err)
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		if n == 0 {
			s.close(io.ErrShortWrite)
			return io.ErrShortWrite
		}
		frame = frame[n:]
	}
	if err := s.conn.SetReadDeadline(time.Now().Add(16 * time.Second)); err != nil {
		s.close(err)
		return err
	}
	return nil
}

func (s *remoteSession) readLoop() {
	reader := bufio.NewReader(s.conn)
	for {
		// Remote v2 sends a ping approximately every five seconds. An idle
		// socket must not expose stale power state indefinitely after resume.
		if err := s.conn.SetReadDeadline(time.Now().Add(16 * time.Second)); err != nil {
			s.close(err)
			return
		}
		n, err := binary.ReadUvarint(reader)
		if err != nil {
			s.close(err)
			return
		}
		if n > 1<<20 {
			s.close(fmt.Errorf("TV control message too large: %d", n))
			return
		}
		b := make([]byte, n)
		if _, err = io.ReadFull(reader, b); err != nil {
			s.close(err)
			return
		}
		msg := new(pb.RemoteMessage)
		if err = proto.Unmarshal(b, msg); err != nil {
			s.close(err)
			return
		}
		if err = s.handle(msg); err != nil {
			s.close(err)
			return
		}
	}
}

func (s *remoteSession) handle(msg *pb.RemoteMessage) error {
	var reply *pb.RemoteMessage
	switch {
	case msg.GetRemoteConfigure() != nil:
		cfg := msg.GetRemoteConfigure()
		device := cfg.GetDeviceInfo()
		s.mu.Lock()
		s.info = &atv.DeviceInfo{Manufacturer: device.GetVendor(), Model: device.GetModel(), SWVersion: device.GetAppVersion()}
		s.mu.Unlock()
		s.features &= cfg.GetCode1()
		reply = &pb.RemoteMessage{RemoteConfigure: &pb.RemoteConfigure{Code1: s.features, DeviceInfo: &pb.RemoteDeviceInfo{Unknown1: 1, Unknown2: "1", PackageName: "atvremote", AppVersion: "1.0.0"}}}
	case msg.GetRemoteSetActive() != nil:
		reply = &pb.RemoteMessage{RemoteSetActive: &pb.RemoteSetActive{Active: s.features}}
	case msg.GetRemotePingRequest() != nil:
		reply = &pb.RemoteMessage{RemotePingResponse: &pb.RemotePingResponse{Val1: msg.GetRemotePingRequest().GetVal1()}}
	case msg.GetRemoteStart() != nil:
		on := msg.GetRemoteStart().GetStarted()
		state := int32(2)
		if on {
			state = 1
		}
		s.power.Store(state)
		s.readyOnce.Do(func() { close(s.ready) })
		s.onPower(on)
	}
	if reply != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		return s.send(ctx, reply)
	}
	return nil
}
