package tv

import (
	"context"
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
	atv "github.com/jkiddo/atvremote/pkg/v2/remote"
)

type Device interface {
	Connect(context.Context) error
	Send(context.Context, string) error
	Power() string
	Close()
}

type Remote struct {
	client   *atv.AndroidTVRemote
	mu       sync.Mutex
	reported string
}

func New(c config.Config, p config.Paths) *Remote { return NewAt(c.Host, p.Certs(c), 6466, 6467) }
func NewAt(host, dir string, apiPort, pairPort int) *Remote {
	r := &Remote{}
	r.client = atv.New("SteamOS TV Control", filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem"), host, atv.WithAPIPort(apiPort), atv.WithPairPort(pairPort), atv.WithIME(false), atv.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))
	r.client.AddIsOnUpdatedCallback(func(on bool) {
		r.mu.Lock()
		defer r.mu.Unlock()
		previous := r.reported
		power := "standby"
		if on {
			power = "on"
		}
		if previous != power {
			slog.Info("TV power state changed", "host", host, "previous", previous, "power", power)
			r.reported = power
		}
	})
	return r
}
func (r *Remote) Power() string {
	// Upstream publishes the protocol state before Connect returns, but invokes
	// its callback afterwards. Read that state directly; callbacks only log.
	on, ok := r.client.IsOn()
	if !ok {
		return "unknown"
	}
	if on {
		return "on"
	}
	return "standby"
}
func (r *Remote) Close() { r.client.Disconnect() }
func (r *Remote) Connect(ctx context.Context) error {
	start := time.Now()
	slog.Debug("Connecting to TV")
	r.Close()
	if err := r.client.Connect(ctx); err != nil {
		r.Close()
		slog.Debug("TV connection failed", "duration_ms", time.Since(start).Milliseconds(), "error", err)
		return err
	}
	slog.Debug("TV connected", "duration_ms", time.Since(start).Milliseconds())
	return nil
}
func (r *Remote) Info() *atv.DeviceInfo { return r.client.DeviceInfo() }

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
	if err := r.bounded(ctx, func() error { return r.client.SendKeyCommand(key) }); err != nil {
		r.Close()
		return err
	}
	return nil
}
func (r *Remote) StartPairing(ctx context.Context) error {
	slog.Info("Starting TV pairing")
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
func InvalidAuth(err error) bool { var auth *atv.InvalidAuth; return errors.As(err, &auth) }
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
