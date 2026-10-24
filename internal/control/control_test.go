package control

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/Tipfurion/steamos-tv-control/internal/config"
)

type fakeTV struct {
	mu       sync.Mutex
	keys     []string
	power    string
	fail     int
	blocked  bool
	connects int
}

func (f *fakeTV) Connect(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.connects++
	if f.fail > 0 {
		f.fail--
		return errors.New("offline")
	}
	return ctx.Err()
}
func (f *fakeTV) Send(ctx context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	f.keys = append(f.keys, key)
	if !f.blocked {
		switch key {
		case "WAKEUP":
			f.power = "on"
		case "SLEEP":
			f.power = "standby"
		case "POWER":
			if f.power == "on" {
				f.power = "standby"
			} else if f.power == "standby" {
				f.power = "on"
			}
		}
	}
	return nil
}
func (f *fakeTV) Power() string { f.mu.Lock(); defer f.mu.Unlock(); return f.power }
func (f *fakeTV) Close()        {}
func fixture() config.Config    { c := config.Default(); c.Host = "tv"; c.InputVerified = true; return c }
func TestGuardedPowerIsIdempotent(t *testing.T) {
	c := fixture()
	c.PowerMode = "guarded-power"
	c.InputVerified = false
	f := &fakeTV{power: "on"}
	ctl := New(c, f)
	ctl.ToggleFile = filepath.Join(t.TempDir(), "pending.json")
	for _, action := range []string{"off", "off", "on", "on"} {
		if err := ctl.Execute(context.Background(), action, 1); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(f.keys, []string{"POWER", "POWER"}) {
		t.Fatal(f.keys)
	}
}
func TestGuardedPowerDoesNotRepeatUnconfirmedToggleAfterRestart(t *testing.T) {
	c := fixture()
	c.PowerMode = "guarded-power"
	f := &fakeTV{power: "on", blocked: true}
	file := filepath.Join(t.TempDir(), "pending.json")
	ctl := New(c, f)
	ctl.ToggleFile = file
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := ctl.Execute(ctx, "off", 1); err != nil {
		t.Fatal(err)
	}
	restarted := New(c, f)
	restarted.ToggleFile = file
	if err := restarted.Execute(context.Background(), "off", 1); err == nil {
		t.Fatal("repeated ambiguous toggle")
	}
	if !reflect.DeepEqual(f.keys, []string{"POWER"}) {
		t.Fatal(f.keys)
	}
}
func TestGuardedPowerRefusesUnknownState(t *testing.T) {
	c := fixture()
	c.PowerMode = "guarded-power"
	f := &fakeTV{power: "unknown"}
	ctl := New(c, f)
	if err := ctl.Execute(context.Background(), "off", 1); err == nil {
		t.Fatal("toggled unknown device")
	}
	if len(f.keys) != 0 {
		t.Fatal(f.keys)
	}
}
func TestWakeOrdersCommandsAndRetries(t *testing.T) {
	f := &fakeTV{power: "standby", fail: 1}
	c := fixture()
	c.MAC = "00:11:22:33:44:55"
	ctl := New(c, f)
	ctl.Retry = time.Millisecond
	wakes := 0
	ctl.Wake = func(context.Context, string, string) error { wakes++; return nil }
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := ctl.Execute(ctx, "on", 1); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.keys, []string{"WAKEUP", "TV_INPUT_HDMI_1"}) || wakes != 1 {
		t.Fatal(f.keys, wakes)
	}
	if !ctl.Status().Confirmed {
		t.Fatal(ctl.Status())
	}
}
func TestSleepIsIdempotentAndNeverWakes(t *testing.T) {
	f := &fakeTV{power: "on"}
	ctl := New(fixture(), f)
	for i := 0; i < 2; i++ {
		if err := ctl.Execute(context.Background(), "off", 1); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(f.keys, []string{"SLEEP"}) {
		t.Fatal(f.keys)
	}
	f.fail = 1
	if err := ctl.Execute(context.Background(), "off", 1); err == nil {
		t.Fatal("offline sleep succeeded")
	}
	if len(f.keys) != 1 {
		t.Fatal(f.keys)
	}
}
func TestCancelBeforePowerReportDoesNotSelectHDMI(t *testing.T) {
	f := &fakeTV{power: "standby", blocked: true}
	ctl := New(fixture(), f)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := ctl.Execute(ctx, "on", 1); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.keys, []string{"WAKEUP"}) {
		t.Fatal(f.keys)
	}
}
func TestUnverifiedInputIsNotAutomated(t *testing.T) {
	c := fixture()
	c.InputVerified = false
	f := &fakeTV{power: "on"}
	ctl := New(c, f)
	if err := ctl.Execute(context.Background(), "on", 1); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.keys, []string{"WAKEUP"}) {
		t.Fatal(f.keys)
	}
}
func TestSequenceOnlyForConfiguredHDMI(t *testing.T) {
	c := fixture()
	c.InputMode = "sequence"
	c.Sequence = []config.Step{{Key: "TV_INPUT"}, {Key: "DPAD_LEFT"}, {Key: "DPAD_CENTER"}}
	f := &fakeTV{power: "on"}
	ctl := New(c, f)
	if err := ctl.Execute(context.Background(), "input", 1); err != nil {
		t.Fatal(err)
	}
	if err := ctl.Execute(context.Background(), "input", 2); err == nil {
		t.Fatal("used sequence for wrong input")
	}
	if !reflect.DeepEqual(f.keys, []string{"TV_INPUT", "DPAD_LEFT", "DPAD_CENTER"}) {
		t.Fatal(f.keys)
	}
}
func TestUnconfirmedSleepReturnsWithinBudget(t *testing.T) {
	f := &fakeTV{power: "on", blocked: true}
	ctl := New(fixture(), f)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := ctl.Execute(ctx, "off", 1); err != nil {
		t.Fatal(err)
	}
	s := ctl.Status()
	if s.Confirmed || s.Result != "command_sent" {
		t.Fatal(s)
	}
}
