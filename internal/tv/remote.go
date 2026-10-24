package tv

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Tipfurion/steamos-tv-control/internal/config"
	pb "github.com/jkiddo/atvremote/pkg/v2/proto"
	atv "github.com/jkiddo/atvremote/pkg/v2/remote"
)

type Device interface {
	Connect(context.Context) error
	Send(context.Context, string) error
	Launch(context.Context, string) error
	Power() string
	Close()
}

type Remote struct {
	client     *atv.AndroidTVRemote
	mu         sync.Mutex
	reported   string
	connectMu  sync.Mutex
	stateMu    sync.Mutex
	session    *remoteSession
	generation uint64
	host, dir  string
	apiPort    int
}

func New(c config.Config, p config.Paths) *Remote { return NewAt(c.Host, p.Certs(c), 6466, 6467) }
func NewAt(host, dir string, apiPort, pairPort int) *Remote {
	r := &Remote{host: host, dir: dir, apiPort: apiPort}
	r.client = atv.New("SteamOS TV Control", filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem"), host, atv.WithAPIPort(apiPort), atv.WithPairPort(pairPort), atv.WithIME(false), atv.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))
	return r
}
func (r *Remote) current() *remoteSession {
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	return r.session
}
func (r *Remote) Power() string {
	if s := r.current(); s != nil {
		return s.Power()
	}
	return "unknown"
}
func (r *Remote) Close() {
	r.stateMu.Lock()
	r.generation++
	s := r.session
	r.session = nil
	r.stateMu.Unlock()
	if s != nil {
		s.close(net.ErrClosed)
	}
	r.client.Disconnect()
}
func (r *Remote) Connect(ctx context.Context) error {
	r.connectMu.Lock()
	defer r.connectMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if s := r.current(); s != nil && s.alive() {
		slog.Debug("Reusing TV control connection")
		return nil
	}
	start := time.Now()
	slog.Debug("Connecting to TV")
	r.Close()
	r.stateMu.Lock()
	generation := r.generation
	r.stateMu.Unlock()
	identity, err := tls.LoadX509KeyPair(filepath.Join(r.dir, "cert.pem"), filepath.Join(r.dir, "key.pem"))
	if err != nil {
		return &atv.InvalidAuth{Message: "missing paired identity", Err: err}
	}
	dialer := tls.Dialer{Config: &tls.Config{Certificates: []tls.Certificate{identity}, InsecureSkipVerify: true, MinVersion: tls.VersionTLS12}}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(r.host, fmt.Sprint(r.apiPort)))
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return controlConnectionError(err)
	}
	var s *remoteSession
	s = newSession(conn, func(on bool) {
		if r.current() != s {
			return
		}
		r.mu.Lock()
		defer r.mu.Unlock()
		power := "standby"
		if on {
			power = "on"
		}
		if r.reported != power {
			slog.Info("TV power state changed", "host", r.host, "previous", r.reported, "power", power)
			r.reported = power
		}
	})
	r.stateMu.Lock()
	if r.generation != generation {
		r.stateMu.Unlock()
		s.close(net.ErrClosed)
		return net.ErrClosed
	}
	r.session = s
	r.stateMu.Unlock()
	go s.readLoop()
	select {
	case <-s.ready:
	case <-s.done:
		return controlConnectionError(s.failure())
	case <-ctx.Done():
		s.close(ctx.Err())
		return ctx.Err()
	}
	if info := s.Info(); info != nil && strings.Contains(strings.ToLower(info.Manufacturer), "tcl") {
		// TCL announces RemoteStart before it accepts keys. Commands sent in
		// that window are silently discarded, even though the write succeeds.
		slog.Debug("Waiting for TCL key receiver", "delay_ms", 250)
		timer := time.NewTimer(250 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			s.close(ctx.Err())
			return ctx.Err()
		case <-s.done:
			return controlConnectionError(s.failure())
		}
	}
	if !s.alive() {
		return controlConnectionError(s.failure())
	}
	slog.Debug("TV connected", "duration_ms", time.Since(start).Milliseconds())
	return nil
}
func controlConnectionError(err error) error {
	if strings.Contains(err.Error(), "tls:") && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		return &atv.InvalidAuth{Message: "TV rejected paired identity", Err: err}
	}
	return &atv.CannotConnect{Message: "TV control connection failed", Err: err}
}
func (r *Remote) Info() *atv.DeviceInfo {
	if s := r.current(); s != nil {
		return s.Info()
	}
	return nil
}

// Upstream writes and pairing reads have no deadlines. Closing the owned
// connection on cancellation also interrupts those blocking operations.
func (r *Remote) bounded(ctx context.Context, fn func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- fn() }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		r.Close()
		return ctx.Err()
	}
}
func (r *Remote) Send(ctx context.Context, key string) error {
	slog.Info("Sending TV key", "key", key)
	code, err := atv.ParseKeyCode(key)
	if err != nil {
		return err
	}
	s := r.current()
	if s == nil {
		return &atv.ConnectionClosed{Message: "TV is not connected"}
	}
	return s.send(ctx, &pb.RemoteMessage{RemoteKeyInject: &pb.RemoteKeyInject{KeyCode: code, Direction: pb.RemoteDirection_SHORT}})
}
func (r *Remote) Launch(ctx context.Context, uri string) error {
	slog.Info("Opening TV input link", "uri", uri)
	s := r.current()
	if s == nil {
		return &atv.ConnectionClosed{Message: "TV is not connected"}
	}
	return s.send(ctx, &pb.RemoteMessage{RemoteAppLinkLaunchRequest: &pb.RemoteAppLinkLaunchRequest{AppLink: uri}})
}
func (r *Remote) StartPairing(ctx context.Context) error {
	slog.Info("Starting TV pairing")
	r.Close()
	return r.bounded(ctx, func() error { return r.client.StartPairing(ctx) })
}
func (r *Remote) FinishPairing(ctx context.Context, pin string) error {
	err := r.bounded(ctx, func() error { return r.client.FinishPairing(strings.ToUpper(strings.TrimSpace(pin))) })
	if err != nil {
		slog.Warn("TV pairing failed", "error", err)
	} else {
		slog.Info("TV pairing completed")
	}
	return err
}
func (r *Remote) Generate(dir string) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	_, err := r.client.GenerateCertIfMissing()
	if err != nil {
		return err
	}
	for _, name := range []string{"cert.pem", "key.pem"} {
		if err := os.Chmod(filepath.Join(dir, name), 0600); err != nil {
			return err
		}
	}
	return nil
}
func InvalidAuth(err error) bool {
	// Upstream wraps every TLS handshake failure as InvalidAuth, including
	// timeouts. A slow or unavailable TV still needs retries and Wake-on-LAN.
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var timeout net.Error
	if errors.As(err, &timeout) && timeout.Timeout() {
		return false
	}
	var auth *atv.InvalidAuth
	return errors.As(err, &auth)
}
func Discover(ctx context.Context) ([]atv.DiscoveredDevice, error) {
	d, err := atv.Discover(ctx, 4*time.Second)
	if err == nil && len(d) > 0 {
		return d, nil
	}
	if native, e := discoverNative(ctx); e == nil && len(native) > 0 {
		return native, nil
	}
	return d, err
}

func MagicPacket(mac string) ([]byte, error) {
	m, err := net.ParseMAC(mac)
	if err != nil || len(m) != 6 {
		return nil, fmt.Errorf("invalid Wake-on-LAN MAC: %q", mac)
	}
	p := make([]byte, 102)
	for i := 0; i < 6; i++ {
		p[i] = 255
	}
	for i := 0; i < 16; i++ {
		copy(p[6+i*6:], m)
	}
	return p, nil
}
func Wake(ctx context.Context, mac, address string) error {
	p, err := MagicPacket(mac)
	if err != nil {
		return err
	}
	d := net.Dialer{}
	conn, err := d.DialContext(ctx, "udp4", address)
	if err != nil {
		return err
	}
	defer conn.Close()
	deadline := time.Now().Add(time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err = conn.SetWriteDeadline(deadline); err != nil {
		return err
	}
	_, err = conn.Write(p)
	return err
}
