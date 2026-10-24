package tv

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/binary"
	"io"
	"net"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	pb "github.com/jkiddo/atvremote/pkg/v2/proto"
	atv "github.com/jkiddo/atvremote/pkg/v2/remote"
	"google.golang.org/protobuf/proto"
)

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
		if err = send(&pb.RemoteMessage{RemoteConfigure: &pb.RemoteConfigure{Code1: 639}}); err != nil {
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
				keys <- press.GetKeyCode()
				if press.GetKeyCode() == pb.RemoteKeyCode_KEYCODE_SLEEP {
					_ = send(&pb.RemoteMessage{RemoteStart: &pb.RemoteStart{Started: false}})
				}
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
	if err = r.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	if r.Power() != "on" {
		t.Fatal(r.Power())
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
