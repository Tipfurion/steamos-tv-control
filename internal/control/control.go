package control

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/Tipfurion/steamos-tv-control/internal/config"
	"github.com/Tipfurion/steamos-tv-control/internal/tv"
)

type Status struct {
	Host      string    `json:"host"`
	Power     string    `json:"power"`
	Action    string    `json:"last_action,omitempty"`
	Result    string    `json:"result,omitempty"`
	Confirmed bool      `json:"confirmed"`
	Error     string    `json:"error,omitempty"`
	Updated   time.Time `json:"updated"`
}
type Controller struct {
	TV         tv.Device
	Config     config.Config
	Wake       func(context.Context, string, string) error
	Retry      time.Duration
	ToggleFile string
	pending    string
	gate       chan struct{}
	mu         sync.RWMutex
	status     Status
}

func New(c config.Config, d tv.Device) *Controller {
	return &Controller{TV: d, Config: c, Wake: tv.Wake, Retry: time.Second, gate: make(chan struct{}, 1), status: Status{Host: c.Host, Power: "unknown"}}
}
func (c *Controller) Status() Status { c.mu.RLock(); defer c.mu.RUnlock(); return c.status }

// Refresh observes state without changing the last action or competing with it.
func (c *Controller) Refresh(ctx context.Context) {
	select {
	case c.gate <- struct{}{}:
		defer func() { <-c.gate }()
	default:
		return
	}
	err := c.TV.Connect(ctx)
	c.mu.Lock()
	defer c.mu.Unlock()
	previous := c.status.Power
	c.status.Power = c.TV.Power()
	if err != nil {
		c.status.Power = "unknown"
	}
	if previous != c.status.Power {
		if err != nil {
			slog.Warn("TV connection lost", "host", c.Config.Host, "error", err)
		} else {
			slog.Info("TV connection restored", "host", c.Config.Host, "power", c.status.Power)
		}
	}
}
func (c *Controller) record(action, result string, confirmed bool, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.status = Status{Host: c.Config.Host, Power: c.TV.Power(), Action: action, Result: result, Confirmed: confirmed, Updated: time.Now()}
	if err != nil {
		c.status.Error = err.Error()
	}
}
func (c *Controller) Execute(ctx context.Context, action string, hdmi int) (err error) {
	start := time.Now()
	select {
	case c.gate <- struct{}{}:
		defer func() { <-c.gate }()
	case <-ctx.Done():
		return ctx.Err()
	}
	confirmed := false
	result := "command_sent"
	defer func() {
		if err != nil {
			result = "failed"
		}
		c.record(action, result, confirmed, err)
		attrs := []any{"host", c.Config.Host, "action", action, "power", c.TV.Power(), "confirmed", confirmed, "duration_ms", time.Since(start).Milliseconds()}
		if err != nil {
			attrs = append(attrs, "error", err)
			slog.Warn("TV action failed", attrs...)
		} else if action == "off" && !confirmed {
			slog.Warn("TV standby command sent without confirmation", attrs...)
		} else {
			slog.Info("TV action completed", attrs...)
		}
	}()
	switch action {
	case "on":
		err = c.on(ctx)
		confirmed = err == nil
	case "off":
		if err = c.TV.Connect(ctx); err != nil {
			return err
		}
		if c.TV.Power() == "standby" {
			if c.Config.PowerMode == "guarded-power" {
				if err = c.clearPending(); err != nil {
					return err
				}
			}
			confirmed = true
			return nil
		}
		if c.Config.PowerMode == "guarded-power" {
			err = c.toggle(ctx, "standby")
		} else {
			err = c.TV.Send(ctx, "SLEEP")
		}
		if err != nil {
			return err
		}
		confirmed = c.waitPower(ctx, "standby") == nil
		if confirmed && c.Config.PowerMode == "guarded-power" {
			err = c.clearPending()
		}
		// A TV may close its socket immediately on sleep. Delivery and a
		// confirmed power report remain distinct in the status interface.
	case "input":
		if hdmi < 1 || hdmi > 4 {
			return fmt.Errorf("HDMI must be between 1 and 4")
		}
		if err = c.TV.Connect(ctx); err != nil {
			return err
		}
		err = c.input(ctx, hdmi)
	case "probe":
		err = c.TV.Connect(ctx)
		result = "state_received"
		confirmed = err == nil
	default:
		return fmt.Errorf("unknown action %q", action)
	}
	return err
}
func (c *Controller) on(ctx context.Context) error {
	var last error
	attempts := 0
	for ctx.Err() == nil {
		attempts++
		attempt, cancel := context.WithTimeout(ctx, 3*time.Second)
		err := c.TV.Connect(attempt)
		cancel()
		if err == nil {
			if c.Config.PowerMode == "guarded-power" {
				if c.TV.Power() != "on" {
					if err = c.toggle(ctx, "on"); err != nil {
						return err
					}
					if err = c.waitPower(ctx, "on"); err != nil {
						return fmt.Errorf("POWER was not confirmed; it will not be retried automatically: %w", err)
					}
				}
				if err = c.clearPending(); err != nil {
					return err
				}
				if c.Config.InputVerified {
					return c.input(ctx, c.Config.HDMI)
				}
				return nil
			}
			if err = c.TV.Send(ctx, "WAKEUP"); err == nil {
				wait, stop := context.WithTimeout(ctx, 4*time.Second)
				err = c.waitPower(wait, "on")
				stop()
				if err == nil {
					if !c.Config.InputVerified {
						return nil
					}
					return c.input(ctx, c.Config.HDMI)
				}
			}
		}
		last = err
		if attempts == 1 {
			slog.Warn("TV is not ready; retrying", "host", c.Config.Host, "error", err)
		} else {
			slog.Debug("TV wake retry", "host", c.Config.Host, "attempt", attempts, "error", err)
		}
		if tv.InvalidAuth(err) {
			return err
		}
		if c.Config.MAC != "" {
			if attempts == 1 {
				slog.Info("Trying Wake-on-LAN backup", "host", c.Config.Host)
			}
			if err = c.Wake(ctx, c.Config.MAC, c.Config.Broadcast); err != nil {
				last = err
			}
		}
		if err = Sleep(ctx, c.Retry); err != nil {
			break
		}
	}
	return fmt.Errorf("TV wake: %v: %w", last, ctx.Err())
}

// Persist an unresolved toggle before sending it. A process restart or repeated
// CLI invocation must not blindly reverse an unconfirmed power transition.
func (c *Controller) toggle(ctx context.Context, target string) error {
	power := c.TV.Power()
	if power != "on" && power != "standby" {
		return fmt.Errorf("POWER requires a current, known power state")
	}
	if c.ToggleFile != "" {
		b, err := os.ReadFile(c.ToggleFile)
		if err == nil {
			var v struct{ Host, Target string }
			if err = json.Unmarshal(b, &v); err != nil {
				return err
			}
			if v.Host == c.Config.Host {
				c.pending = v.Target
			}
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	if c.pending != "" {
		return fmt.Errorf("previous POWER (%s) was not confirmed; repeat blocked", c.pending)
	}
	if c.ToggleFile != "" {
		b, _ := json.Marshal(struct{ Host, Target string }{c.Config.Host, target})
		if err := config.AtomicWrite(c.ToggleFile, b, 0600); err != nil {
			return err
		}
	}
	c.pending = target
	return c.TV.Send(ctx, "POWER")
}
func (c *Controller) clearPending() error {
	if c.ToggleFile != "" {
		if err := os.Remove(c.ToggleFile); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	c.pending = ""
	return nil
}
func (c *Controller) input(ctx context.Context, hdmi int) error {
	if c.Config.InputMode == "direct" {
		return c.TV.Send(ctx, fmt.Sprintf("TV_INPUT_HDMI_%d", hdmi))
	}
	if hdmi != c.Config.HDMI {
		return fmt.Errorf("the sequence is configured only for HDMI %d", c.Config.HDMI)
	}
	for _, s := range c.Config.Sequence {
		if err := c.TV.Send(ctx, s.Key); err != nil {
			return err
		}
		if err := Sleep(ctx, time.Duration(s.DelayMS)*time.Millisecond); err != nil {
			return err
		}
	}
	return nil
}
func (c *Controller) waitPower(ctx context.Context, power string) error {
	for {
		if c.TV.Power() == power {
			return nil
		}
		if err := Sleep(ctx, 50*time.Millisecond); err != nil {
			return err
		}
	}
}
func Sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
