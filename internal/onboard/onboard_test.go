package onboard

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Tipfurion/steamos-tv-control/internal/config"
	atv "github.com/jkiddo/atvremote/pkg/v2/remote"
)

type fakeRemote struct {
	pin       string
	keys      []string
	pairError error
}

func (f *fakeRemote) Connect(context.Context) error { return nil }
func (f *fakeRemote) Send(_ context.Context, key string) error {
	f.keys = append(f.keys, key)
	return nil
}
func (f *fakeRemote) Power() string { return "on" }
func (f *fakeRemote) Close()        {}
func (f *fakeRemote) Generate(dir string) error {
	return os.WriteFile(filepath.Join(dir, "key.pem"), []byte("test identity"), 0600)
}
func (f *fakeRemote) StartPairing(context.Context) error { return nil }
func (f *fakeRemote) FinishPairing(_ context.Context, pin string) error {
	f.pin = pin
	return f.pairError
}
func (f *fakeRemote) Info() *atv.DeviceInfo {
	return &atv.DeviceInfo{Manufacturer: "TCL", Model: "65C7L"}
}
func paths(t *testing.T) config.Paths {
	d := t.TempDir()
	return config.Paths{Config: filepath.Join(d, "config"), Data: filepath.Join(d, "data"), State: filepath.Join(d, "state")}
}
func options(f *fakeRemote, devices ...atv.DiscoveredDevice) Options {
	return Options{Discover: func(context.Context) ([]atv.DiscoveredDevice, error) { return devices, nil }, NewRemote: func(config.Config, config.Paths) Remote { return f }, LearnMAC: func(string) string { return "00:11:22:33:44:55" }}
}
func TestSingleTVAutomaticallyConfiguredWithoutTUI(t *testing.T) {
	p := paths(t)
	f := &fakeRemote{}
	var out bytes.Buffer
	err := Configure(context.Background(), p, strings.NewReader("a1b2c3\n\n"), &out, options(f, atv.DiscoveredDevice{Name: "Living Room", IP: "192.0.2.1"}))
	if err != nil {
		t.Fatal(err)
	}
	c, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.Host != "192.0.2.1" || c.PowerMode != "guarded-power" || !c.AutoOn || !c.AutoOff || c.InputVerified || c.MAC == "" {
		t.Fatal(c)
	}
	if f.pin != "a1b2c3" || len(f.keys) != 0 {
		t.Fatal(f)
	}
	if strings.Contains(out.String(), "\x1b") || strings.Contains(out.String(), "a1b2c3") {
		t.Fatal("TUI or pairing code in output", out.String())
	}
}
func TestMultipleTVsAndConfirmedHDMI(t *testing.T) {
	p := paths(t)
	f := &fakeRemote{}
	var out bytes.Buffer
	err := Configure(context.Background(), p, strings.NewReader("2\na1b2c3\n3\ny\n"), &out, options(f, atv.DiscoveredDevice{IP: "192.0.2.1"}, atv.DiscoveredDevice{IP: "192.0.2.2"}))
	if err != nil {
		t.Fatal(err)
	}
	c, err := config.Load(p)
	if err != nil || c.Host != "192.0.2.2" || c.HDMI != 3 || !c.InputVerified {
		t.Fatal(c, err)
	}
	if !reflect.DeepEqual(f.keys, []string{"TV_INPUT_HDMI_3"}) {
		t.Fatal(f.keys)
	}
}
func TestFailedPairingDoesNotSaveConfiguration(t *testing.T) {
	p := paths(t)
	f := &fakeRemote{pairError: errors.New("rejected")}
	var out bytes.Buffer
	err := Configure(context.Background(), p, strings.NewReader("badpin\n"), &out, options(f, atv.DiscoveredDevice{IP: "192.0.2.1"}))
	if err == nil {
		t.Fatal("pairing failure ignored")
	}
	if _, err = os.Stat(p.File()); !os.IsNotExist(err) {
		t.Fatal("partial config saved")
	}
	entries, _ := os.ReadDir(p.Data)
	if len(entries) != 0 {
		t.Fatal("unused identity retained")
	}
}
