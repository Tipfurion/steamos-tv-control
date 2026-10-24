package logind

import (
	"bufio"
	"context"
	"io"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/Tipfurion/steamos-tv-control/internal/automation"
	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
	"github.com/godbus/dbus/v5/prop"
)

type pipeLock struct {
	what string
	r, w *os.File
}
type mockManager struct{ locks chan pipeLock }

func (m *mockManager) Inhibit(what, who, why, mode string) (dbus.UnixFD, *dbus.Error) {
	r, w, err := os.Pipe()
	if err != nil {
		return 0, dbus.MakeFailedError(err)
	}
	m.locks <- pipeLock{what, r, w}
	return dbus.UnixFD(w.Fd()), nil
}
func fixtureBus(t *testing.T, metadata bool) (*Manager, *dbus.Conn, *mockManager) {
	t.Helper()
	if _, err := exec.LookPath("dbus-daemon"); err != nil {
		t.Skip("dbus-daemon required; exercised in Linux Docker/CI")
	}
	cmd := exec.Command("dbus-daemon", "--session", "--nofork", "--print-address=1")
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	scan := bufio.NewScanner(out)
	if !scan.Scan() {
		t.Fatal("no D-Bus address")
	}
	address := scan.Text()
	server, err := dbus.Connect(address)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { server.Close() })
	if _, err = server.RequestName(destination, dbus.NameFlagDoNotQueue); err != nil {
		t.Fatal(err)
	}
	mock := &mockManager{locks: make(chan pipeLock, 16)}
	if err = server.Export(mock, objectPath, iface); err != nil {
		t.Fatal(err)
	}
	signals := []introspect.Signal{{Name: "PrepareForSleep"}, {Name: "PrepareForShutdown"}}
	if metadata {
		signals = append(signals, introspect.Signal{Name: "PrepareForShutdownWithMetadata"})
	}
	node := &introspect.Node{Interfaces: []introspect.Interface{{Name: iface, Methods: introspect.Methods(mock), Signals: signals}}}
	if err = server.Export(introspect.NewIntrospectable(node), objectPath, "org.freedesktop.DBus.Introspectable"); err != nil {
		t.Fatal(err)
	}
	if _, err = prop.Export(server, objectPath, prop.Map{iface: {"InhibitDelayMaxUSec": &prop.Prop{Value: uint64(500000), Writable: false, Emit: prop.EmitFalse}}}); err != nil {
		t.Fatal(err)
	}
	m, err := Open(address)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	return m, server, mock
}
func TestRealDBusSignalsAndInhibitorFDLifetime(t *testing.T) {
	m, server, mock := fixtureBus(t, true)
	if !m.Metadata {
		t.Fatal("metadata not detected")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	events := make(chan automation.Event, 4)
	errs := make(chan error, 1)
	go func() { errs <- m.Watch(ctx, events) }()
	select {
	case <-m.Ready:
	case err := <-errs:
		t.Fatal(err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	locks := map[string]pipeLock{}
	for i := 0; i < 2; i++ {
		v := <-mock.locks
		locks[v.what] = v
		t.Cleanup(func() { v.r.Close(); v.w.Close() })
	}
	lock := locks["sleep"]
	lock.w.Close()
	eof := make(chan error, 1)
	go func() { _, err := lock.r.Read(make([]byte, 1)); eof <- err }()
	if err := server.Emit(objectPath, iface+".PrepareForSleep", true); err != nil {
		t.Fatal(err)
	}
	var event automation.Event
	select {
	case event = <-events:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if event.Kind != "sleep" {
		t.Fatal(event)
	}
	select {
	case <-eof:
		t.Fatal("inhibitor released before action completion")
	default:
	}
	close(event.Done)
	select {
	case err := <-eof:
		if err != io.EOF {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("inhibitor FD leaked")
	}
	server.Emit(objectPath, iface+".PrepareForSleep", false)
	select {
	case event = <-events:
		if event.Kind != "resume" {
			t.Fatal(event)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	newLock := <-mock.locks
	defer newLock.r.Close()
	defer newLock.w.Close()
	if newLock.what != "sleep" {
		t.Fatal("did not reacquire sleep inhibitor")
	}
	server.Emit(objectPath, iface+".PrepareForShutdownWithMetadata", true, map[string]dbus.Variant{"type": dbus.MakeVariant("reboot")})
	select {
	case event = <-events:
		if event.Kind != "reboot" {
			t.Fatal(event)
		}
		close(event.Done)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	cancel()
	if err := <-errs; err != nil {
		t.Fatal(err)
	}
}
func TestOldLogindDoesNotGuessPoweroff(t *testing.T) {
	m, server, mock := fixtureBus(t, false)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	events := make(chan automation.Event, 1)
	go m.Watch(ctx, events)
	<-m.Ready
	for i := 0; i < 2; i++ {
		l := <-mock.locks
		defer l.r.Close()
		defer l.w.Close()
	}
	server.Emit(objectPath, iface+".PrepareForShutdown", true)
	select {
	case e := <-events:
		if e.Kind != "unknown-shutdown" {
			t.Fatal(e.Kind)
		}
		close(e.Done)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}
