package tv

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"path/filepath"
	"reflect"
	"syscall"
	"testing"
	"time"

	pb "github.com/jkiddo/atvremote/pkg/v2/proto"
	atv "github.com/jkiddo/atvremote/pkg/v2/remote"
	"google.golang.org/protobuf/proto"
)

func TestAuthenticationClassification(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"rejected certificate", &atv.InvalidAuth{Message: "need to pair", Err: errors.New("remote error: tls: unknown certificate")}, true},
		{"TLS deadline", &atv.InvalidAuth{Message: "need to pair", Err: context.DeadlineExceeded}, false},
		{"TLS cancellation", &atv.InvalidAuth{Message: "need to pair", Err: context.Canceled}, false},
		{"network timeout", &atv.InvalidAuth{Message: "need to pair", Err: &net.DNSError{IsTimeout: true}}, false},
		{"connection refused", &atv.CannotConnect{Message: "connection refused"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := InvalidAuth(tc.err); got != tc.want {
				t.Fatalf("InvalidAuth(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestWakePacketDelivered(t *testing.T) {
	ln, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	ln.SetReadDeadline(time.Now().Add(time.Second))
	if err = Wake(context.Background(), "00:11:22:33:44:55", ln.LocalAddr().String()); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 200)
	n, _, err := ln.ReadFrom(buf)
	if err != nil {
		t.Fatal(err)
	}
	if n != 102 {
		t.Fatal(n)
	}
	for i := 0; i < 6; i++ {
		if buf[i] != 255 {
			t.Fatal("missing magic prefix")
		}
	}
	for i := 0; i < 16; i++ {
		if !reflect.DeepEqual(buf[6+i*6:12+i*6], []byte{0, 17, 34, 51, 68, 85}) {
			t.Fatal("invalid repeated MAC")
		}
	}
}

func TestPairingCancellationClosesSilentServer(t *testing.T) {
	cert, key, err := atv.GenerateSelfSignedCert("silent TV")
	if err != nil {
		t.Fatal(err)
	}
	identity, err := tls.X509KeyPair(cert, key)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{identity}, ClientAuth: tls.RequireAnyClientCert, MinVersion: tls.VersionTLS12})
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	closed := make(chan struct{})
	go func() {
		defer close(closed)
		conn, e := ln.Accept()
		if e != nil {
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(2 * time.Second))
		io.Copy(io.Discard, conn)
	}()
	dir := t.TempDir()
	r := NewAt("127.0.0.1", dir, 6466, ln.Addr().(*net.TCPAddr).Port)
	defer r.Close()
	if err = r.Generate(dir); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err = r.StartPairing(ctx); err == nil {
		t.Fatal("silent pairing succeeded")
	}
	if time.Since(start) > time.Second {
		t.Fatal("pairing ignored deadline")
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("canceled pairing connection leaked")
	}
}
func TestRemoteTLSFramingAndPower(t *testing.T) {
	t.Run("normal callback", func(t *testing.T) { testRemoteTLSFramingAndPower(t, false, "") })
	t.Run("delayed callback", func(t *testing.T) { testRemoteTLSFramingAndPower(t, true, "") })
	t.Run("TCL key receiver starts late", func(t *testing.T) { testRemoteTLSFramingAndPower(t, false, "TCL") })
}

func testRemoteTLSFramingAndPower(t *testing.T, delayCallback bool, manufacturer string) {
	cert, key, err := atv.GenerateSelfSignedCert("test TV")
	if err != nil {
		t.Fatal(err)
	}
	identity, err := tls.X509KeyPair(cert, key)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{identity}, ClientAuth: tls.RequireAnyClientCert, MinVersion: tls.VersionTLS12})
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	keys := make(chan pb.RemoteKeyCode, 4)
	links := make(chan string, 1)
	fail := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			fail <- err
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(3 * time.Second))
		reader := bufio.NewReader(conn)
		send := func(m *pb.RemoteMessage) error {
			b, e := proto.Marshal(m)
			if e != nil {
				return e
			}
			framed := binary.AppendUvarint(nil, uint64(len(b)))
			framed = append(framed, b...)
			for _, v := range framed {
				if _, e = conn.Write([]byte{v}); e != nil {
					return e
				}
			}
			return nil
		}
		recv := func() (*pb.RemoteMessage, error) {
			n, e := binary.ReadUvarint(reader)
			if e != nil {
				return nil, e
			}
			if n > 1<<20 {
				return nil, io.ErrShortBuffer
			}
			b := make([]byte, n)
			if _, e = io.ReadFull(reader, b); e != nil {
				return nil, e
			}
			m := new(pb.RemoteMessage)
			return m, proto.Unmarshal(b, m)
		}
		if err = send(&pb.RemoteMessage{RemoteConfigure: &pb.RemoteConfigure{Code1: 639, DeviceInfo: &pb.RemoteDeviceInfo{Vendor: manufacturer}}}); err != nil {
			fail <- err
			return
		}
		if _, err = recv(); err != nil {
			fail <- err
			return
		}
		if err = send(&pb.RemoteMessage{RemoteSetActive: &pb.RemoteSetActive{Active: 639}}); err != nil {
			fail <- err
			return
		}
		if _, err = recv(); err != nil {
			fail <- err
			return
		}
		started := time.Now()
		if err = send(&pb.RemoteMessage{RemoteStart: &pb.RemoteStart{Started: true}}); err != nil {
			fail <- err
			return
		}
		for {
			m, e := recv()
			if e != nil {
				return
			}
			if press := m.GetRemoteKeyInject(); press != nil {
				if manufacturer == "TCL" && time.Since(started) < 200*time.Millisecond {
					// Mirror the real TV: transport writes succeed while keys
					// arriving just after RemoteStart have no effect.
					continue
				}
				keys <- press.GetKeyCode()
				if press.GetKeyCode() == pb.RemoteKeyCode_KEYCODE_SLEEP {
					_ = send(&pb.RemoteMessage{RemoteStart: &pb.RemoteStart{Started: false}})
				}
			}
			if link := m.GetRemoteAppLinkLaunchRequest(); link != nil {
				links <- link.GetAppLink()
			}
		}
	}()
	dir := filepath.Join(t.TempDir(), "identity")
	r := NewAt("127.0.0.1", dir, ln.Addr().(*net.TCPAddr).Port, 6467)
	defer r.Close()
	if err = r.Generate(dir); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if delayCallback {
		// Block logging while the control connection becomes ready. Power
		// must still expose the protocol state immediately.
		func() {
			r.mu.Lock()
			defer r.mu.Unlock()
			if err = r.Connect(ctx); err != nil {
				t.Fatal(err)
			}
			power := make(chan string, 1)
			go func() { power <- r.Power() }()
			select {
			case value := <-power:
				if value != "on" {
					t.Fatal(value)
				}
			case <-ctx.Done():
				t.Fatal("power reporting waits for callback")
			}
		}()
	} else if err = r.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	if r.Power() != "on" {
		t.Fatal(r.Power())
	}
	// Model the pre-suspend window: new TCP connections cannot be opened,
	// but the already established control socket can still carry standby.
	ln.Close()
	reuse, stop := context.WithTimeout(ctx, 30*time.Millisecond)
	if err = r.Connect(reuse); err != nil {
		stop()
		t.Fatal("ready connection was not reused", err)
	}
	stop()
	const uri = "content://android.media.tv/passthrough/com.tcl.tvinput%2F.TvPassThroughService%2FHW16"
	if err = r.Launch(ctx, uri); err != nil {
		t.Fatal(err)
	}
	select {
	case link := <-links:
		if link != uri {
			t.Fatal("input URI changed on the wire", link)
		}
	case err := <-fail:
		t.Fatal(err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err = r.Send(ctx, "SLEEP"); err != nil {
		t.Fatal(err)
	}
	select {
	case key := <-keys:
		if key != pb.RemoteKeyCode_KEYCODE_SLEEP {
			t.Fatal(key)
		}
	case err := <-fail:
		t.Fatal(err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	for r.Power() != "standby" {
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(time.Millisecond):
		}
	}
}

func TestControlSessionDetectsDisconnectAndReconnects(t *testing.T) {
	cert, key, err := atv.GenerateSelfSignedCert("test TV")
	if err != nil {
		t.Fatal(err)
	}
	identity, err := tls.X509KeyPair(cert, key)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{identity}, ClientAuth: tls.RequireAnyClientCert, MinVersion: tls.VersionTLS12})
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	closePeer := make(chan struct{})
	finished := make(chan error, 1)
	go func() {
		for _, on := range []bool{true, false} {
			conn, err := ln.Accept()
			if err != nil {
				finished <- err
				return
			}
			conn.SetDeadline(time.Now().Add(3 * time.Second))
			reader := bufio.NewReader(conn)
			send := func(msg *pb.RemoteMessage) error {
				b, e := proto.Marshal(msg)
				if e != nil {
					return e
				}
				_, e = conn.Write(append(binary.AppendUvarint(nil, uint64(len(b))), b...))
				return e
			}
			recv := func() error {
				n, e := binary.ReadUvarint(reader)
				if e != nil {
					return e
				}
				if n > 1<<20 {
					return io.ErrShortBuffer
				}
				_, e = io.CopyN(io.Discard, reader, int64(n))
				return e
			}
			for _, msg := range []*pb.RemoteMessage{
				{RemoteConfigure: &pb.RemoteConfigure{Code1: 639}},
				{RemoteSetActive: &pb.RemoteSetActive{Active: 639}},
			} {
				if err = send(msg); err == nil {
					err = recv()
				}
				if err != nil {
					conn.Close()
					finished <- err
					return
				}
			}
			if err = send(&pb.RemoteMessage{RemoteStart: &pb.RemoteStart{Started: on}}); err != nil {
				conn.Close()
				finished <- err
				return
			}
			if on {
				<-closePeer
			} else {
				io.Copy(io.Discard, reader)
			}
			conn.Close()
		}
		finished <- nil
	}()
	dir := t.TempDir()
	r := NewAt("127.0.0.1", dir, ln.Addr().(*net.TCPAddr).Port, 6467)
	defer r.Close()
	if err = r.Generate(dir); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err = r.Connect(ctx); err != nil || r.Power() != "on" {
		t.Fatal(err, r.Power())
	}
	close(closePeer)
	for r.Power() != "unknown" {
		select {
		case <-ctx.Done():
			t.Fatal("cached power survived peer EOF")
		case <-time.After(time.Millisecond):
		}
	}
	if err = r.Connect(ctx); err != nil || r.Power() != "standby" {
		t.Fatal(err, r.Power())
	}
	r.Close()
	if r.Power() != "unknown" {
		t.Fatal("closed session retained power")
	}
	select {
	case err = <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

func TestControlSessionAnswersPing(t *testing.T) {
	client, server := net.Pipe()
	s := newSession(client, func(bool) {})
	defer s.close(net.ErrClosed)
	defer server.Close()
	server.SetDeadline(time.Now().Add(time.Second))
	go s.readLoop()
	b, err := proto.Marshal(&pb.RemoteMessage{RemotePingRequest: &pb.RemotePingRequest{Val1: 1234}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = server.Write(append(binary.AppendUvarint(nil, uint64(len(b))), b...)); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(server)
	n, err := binary.ReadUvarint(reader)
	if err != nil {
		t.Fatal(err)
	}
	if n > 1<<20 {
		t.Fatal("oversized response")
	}
	b = make([]byte, n)
	if _, err = io.ReadFull(reader, b); err != nil {
		t.Fatal(err)
	}
	msg := new(pb.RemoteMessage)
	if err = proto.Unmarshal(b, msg); err != nil || msg.GetRemotePingResponse().GetVal1() != 1234 {
		t.Fatal(msg, err)
	}
}

func TestAbandonedControlConnectionIsReset(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	client, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	server, err := ln.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	s := newSession(client, func(bool) {})
	s.close(net.ErrClosed)
	server.SetReadDeadline(time.Now().Add(time.Second))
	_, err = server.Read(make([]byte, 1))
	if !errors.Is(err, syscall.ECONNRESET) {
		t.Fatal("abandoned connection was closed gracefully", err)
	}
}
