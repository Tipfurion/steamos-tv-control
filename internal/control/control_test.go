package control

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/Tipfurion/steamos-tv-control/internal/config"
)

type fakeTV struct {
	mu       sync.Mutex
	keys     []string
	links    []string
	power    string
	fail     int
	blocked  bool
	connects int
	closes   int
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
func (f *fakeTV) Launch(ctx context.Context, uri string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	f.links = append(f.links, uri)
	return nil
}
func (f *fakeTV) Power() string { f.mu.Lock(); defer f.mu.Unlock(); return f.power }
func (f *fakeTV) Close()        { f.mu.Lock(); defer f.mu.Unlock(); f.closes++ }
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
	synctest.Test(t, func(t *testing.T) {
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
	})
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

func TestGuardedPowerAcceptsDelayedConfirmationAfterRestart(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := fixture()
		c.PowerMode = "guarded-power"
		c.InputVerified = false
		f := &fakeTV{power: "on", blocked: true}
		file := filepath.Join(t.TempDir(), "pending.json")
		ctl := New(c, f)
		ctl.ToggleFile = file
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		if err := ctl.Execute(ctx, "off", 1); err != nil {
			t.Fatal(err)
		}
		// Standby arrives after the sleep budget expired. A fresh connection on
		// the next invocation confirms the previous toggle before waking the TV.
		f.mu.Lock()
		f.power = "standby"
		f.blocked = false
		f.mu.Unlock()
		restarted := New(c, f)
		restarted.ToggleFile = file
		if err := restarted.Execute(context.Background(), "on", 1); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(f.keys, []string{"POWER", "POWER"}) || !restarted.Status().Confirmed {
			t.Fatal(f.keys, restarted.Status())
		}
	})
}

func TestWakeOrdersCommandsAndRetries(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
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
	})
}

func TestGuardedWakeFallsBackWithoutRepeatingPower(t *testing.T) {
	for _, previousAttempt := range []bool{false, true} {
		t.Run(map[bool]string{false: "ignored POWER", true: "unconfirmed previous invocation"}[previousAttempt], func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				c := fixture()
				c.PowerMode = "guarded-power"
				c.InputVerified = false
				f := &fakeTV{power: "standby", blocked: true}
				file := filepath.Join(t.TempDir(), "pending.json")
				if previousAttempt {
					ctl := New(c, f)
					ctl.ToggleFile = file
					ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
					err := ctl.Execute(ctx, "on", 1)
					cancel()
					if !errors.Is(err, context.DeadlineExceeded) {
						t.Fatal(err)
					}
				}
				c.MAC = "00:11:22:33:44:55"
				ctl := New(c, f)
				ctl.ToggleFile = file
				ctl.Retry = time.Millisecond
				wakes := 0
				ctl.Wake = func(context.Context, string, string) error {
					wakes++
					f.mu.Lock()
					f.power = "on"
					f.mu.Unlock()
					return nil
				}
				ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
				defer cancel()
				if err := ctl.Execute(ctx, "on", 1); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(f.keys, []string{"POWER"}) || wakes != 1 || !ctl.Status().Confirmed {
					t.Fatal(f.keys, wakes, ctl.Status())
				}
			})
		})
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
	synctest.Test(t, func(t *testing.T) {
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
	})
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

func TestInputLinkIsUsedForManualSelectionAndWake(t *testing.T) {
	const uri = "content://android.media.tv/passthrough/com.tcl.tvinput%2F.TvPassThroughService%2FHW16"
	for _, powerMode := range []string{"discrete", "guarded-power"} {
		t.Run(powerMode, func(t *testing.T) {
			c := fixture()
			c.HDMI = 2
			c.InputMode = "app-link"
			c.InputURI = uri
			c.PowerMode = powerMode
			f := &fakeTV{power: "on"}
			ctl := New(c, f)
			if err := ctl.Execute(context.Background(), "input", 2); err != nil {
				t.Fatal(err)
			}
			if err := ctl.Execute(context.Background(), "input", 1); err == nil {
				t.Fatal("used HDMI 2 link for another port")
			}
			if len(f.keys) != 0 || !reflect.DeepEqual(f.links, []string{uri}) {
				t.Fatal(f.keys, f.links)
			}
			f.power = "standby"
			if err := ctl.Execute(context.Background(), "on", 2); err != nil {
				t.Fatal(err)
			}
			wantKey := "WAKEUP"
			if powerMode == "guarded-power" {
				wantKey = "POWER"
			}
			if !reflect.DeepEqual(f.keys, []string{wantKey}) || !reflect.DeepEqual(f.links, []string{uri, uri}) {
				t.Fatal(f.keys, f.links)
			}
			ctl.Config.InputVerified = false
			if err := ctl.Execute(context.Background(), "on", 2); err != nil {
				t.Fatal(err)
			}
			if len(f.links) != 2 {
				t.Fatal("unverified input link was launched automatically")
			}
		})
	}
}

func TestUnconfirmedSleepReturnsWithinBudget(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
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
		if f.closes == 0 {
			t.Fatal("unconfirmed standby left an old connection open")
		}
	})
}
