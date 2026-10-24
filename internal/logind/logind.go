package logind

import (
	"context"
	"encoding/xml"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/Tipfurion/steamos-tv-control/internal/automation"
	"github.com/godbus/dbus/v5"
)

const destination = "org.freedesktop.login1"
const objectPath = dbus.ObjectPath("/org/freedesktop/login1")
const iface = "org.freedesktop.login1.Manager"

type Manager struct {
	conn     *dbus.Conn
	obj      dbus.BusObject
	Metadata bool
	Budget   time.Duration
	Ready    chan struct{}
}

func Open(address string) (*Manager, error) {
	var conn *dbus.Conn
	var err error
	if address == "" {
		conn, err = dbus.ConnectSystemBus()
	} else {
		conn, err = dbus.Connect(address)
	}
	if err != nil {
		return nil, err
	}
	m := &Manager{conn: conn, obj: conn.Object(destination, objectPath), Ready: make(chan struct{})}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var raw string
	if err = m.obj.CallWithContext(ctx, "org.freedesktop.DBus.Introspectable.Introspect", 0).Store(&raw); err != nil {
		conn.Close()
		return nil, err
	}
	var root struct {
		Interfaces []struct {
			Name    string `xml:"name,attr"`
			Signals []struct {
				Name string `xml:"name,attr"`
			} `xml:"signal"`
		} `xml:"interface"`
	}
	if err = xml.Unmarshal([]byte(raw), &root); err != nil {
		conn.Close()
		return nil, err
	}
	for _, i := range root.Interfaces {
		if i.Name == iface {
			for _, s := range i.Signals {
				if s.Name == "PrepareForShutdownWithMetadata" {
					m.Metadata = true
				}
			}
		}
	}
	v, err := m.obj.GetProperty(iface + ".InhibitDelayMaxUSec")
	if err != nil {
		conn.Close()
		return nil, err
	}
	var usec uint64
	if err = v.Store(&usec); err != nil {
		conn.Close()
		return nil, err
	}
	m.Budget = min(2*time.Second, max(0, time.Duration(usec)*time.Microsecond-250*time.Millisecond))
	return m, nil
}
func (m *Manager) Close() { m.conn.Close() }
func (m *Manager) inhibit(ctx context.Context, what string) (*os.File, error) {
	var fd dbus.UnixFD
	err := m.obj.CallWithContext(ctx, iface+".Inhibit", 0, what, "SteamOS TV Control", "Send TV standby before PC sleeps", "delay").Store(&fd)
	if err != nil {
		return nil, fmt.Errorf("logind %s inhibitor: %w", what, err)
	}
	return os.NewFile(uintptr(fd), "logind-"+what), nil
}

func (m *Manager) Watch(ctx context.Context, out chan<- automation.Event) error {
	shutdown := "PrepareForShutdown"
	if m.Metadata {
		shutdown = "PrepareForShutdownWithMetadata"
	} else {
		slog.Warn("logind lacks shutdown metadata; automatic TV standby on shutdown disabled")
	}
	for _, member := range []string{"PrepareForSleep", shutdown} {
		if err := m.conn.AddMatchSignal(dbus.WithMatchObjectPath(objectPath), dbus.WithMatchInterface(iface), dbus.WithMatchMember(member)); err != nil {
			return err
		}
	}
	signals := make(chan *dbus.Signal, 16)
	m.conn.Signal(signals)
	defer m.conn.RemoveSignal(signals)
	locks := map[string]*os.File{}
	defer func() {
		for _, f := range locks {
			if f != nil {
				f.Close()
			}
		}
	}()
	acquire := func(what string) error {
		call, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		f, err := m.inhibit(call, what)
		if err == nil {
			locks[what] = f
			slog.Debug("Delay inhibitor acquired", "operation", what)
		}
		return err
	}
	for _, what := range []string{"sleep", "shutdown"} {
		if err := acquire(what); err != nil {
			return err
		}
	}
	close(m.Ready)
	slog.Info("Listening for PC power events", "shutdown_metadata", m.Metadata, "sleep_budget_ms", m.Budget.Milliseconds())
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-m.conn.Context().Done():
			return fmt.Errorf("system D-Bus connection lost")
		case sig, ok := <-signals:
			if !ok {
				return fmt.Errorf("logind signal stream closed")
			}
			if len(sig.Body) == 0 {
				continue
			}
			start, ok := sig.Body[0].(bool)
			if !ok {
				continue
			}
			what := "shutdown"
			sleep := strings.HasSuffix(sig.Name, ".PrepareForSleep")
			if sleep {
				what = "sleep"
			}
			if !start {
				slog.Info("PC power transition finished", "operation", what)
				if locks[what] != nil {
					locks[what].Close()
				}
				if err := acquire(what); err != nil {
					return err
				}
				if sleep {
					select {
					case out <- automation.Event{Kind: "resume"}:
					case <-ctx.Done():
						return nil
					}
				}
				continue
			}
			kind := "unknown-shutdown"
			if sleep {
				kind = "sleep"
			} else if m.Metadata && len(sig.Body) > 1 {
				if meta, ok := sig.Body[1].(map[string]dbus.Variant); ok {
					switch meta["type"].Value() {
					case "power-off", "halt":
						kind = "poweroff"
					case "reboot", "soft-reboot", "kexec":
						kind = "reboot"
					}
				}
			}
			e := automation.Event{Kind: kind, Budget: m.Budget, Deadline: time.Now().Add(m.Budget), Done: make(chan struct{})}
			slog.Info("PC power transition requested", "event", kind, "budget_ms", m.Budget.Milliseconds())
			// The deadline includes queueing time, not just the network call.
			timer := time.NewTimer(m.Budget)
			select {
			case out <- e:
				select {
				case <-e.Done:
				case <-timer.C:
				case <-ctx.Done():
				}
			case <-timer.C:
			case <-ctx.Done():
			}
			timer.Stop()
			if locks[what] != nil {
				locks[what].Close()
				locks[what] = nil
				slog.Debug("Delay inhibitor released", "operation", what)
			}
		}
	}
}
