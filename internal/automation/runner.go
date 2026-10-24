package automation

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Tipfurion/steamos-tv-control/internal/config"
)

type Event struct {
	Kind     string
	Budget   time.Duration
	Deadline time.Time
	Done     chan struct{}
}
type Executor interface {
	Execute(context.Context, string, int) error
	ResetConnection()
}

// Run serializes lifecycle intents. A new intent cancels the previous one;
// the controller also serializes access to the physical remote connection.
func Run(ctx context.Context, events <-chan Event, c config.Config, exec Executor) {
	var cancel context.CancelFunc
	defer func() {
		if cancel != nil {
			cancel()
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case e, ok := <-events:
			if !ok {
				return
			}
			if cancel != nil {
				cancel()
			}
			action := ""
			timeout := time.Duration(c.WakeSeconds) * time.Second
			switch e.Kind {
			case "resume":
				exec.ResetConnection()
				if c.AutoOn {
					action = "on"
				}
			case "boot":
				if c.AutoOn {
					action = "on"
				}
			case "sleep", "poweroff":
				if c.AutoOff {
					action = "off"
				}
				timeout = min(2*time.Second, e.Budget)
			case "reboot", "unknown-shutdown":
			}
			if !e.Deadline.IsZero() {
				timeout = min(timeout, time.Until(e.Deadline))
			}
			job, stop := context.WithTimeout(ctx, max(timeout, 0))
			cancel = stop
			go func(e Event, action string, job context.Context, stop context.CancelFunc) {
				defer stop()
				if e.Done != nil {
					defer close(e.Done)
				}
				if action != "" {
					err := exec.Execute(job, action, c.HDMI)
					if err != nil && !errors.Is(err, context.Canceled) {
						slog.Warn("TV lifecycle action", "event", e.Kind, "error", err)
					} else {
						slog.Info("TV lifecycle action", "event", e.Kind, "action", action)
					}
				}
			}(e, action, job, stop)
		}
	}
}

// ClaimBoot marks before issuing commands, preventing a crash/restart loop
// from waking the TV repeatedly within the same OS boot.
func ClaimBoot(p config.Paths, id string) (bool, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return false, errors.New("empty boot id")
	}
	path := filepath.Join(p.State, "last-boot")
	b, err := os.ReadFile(path)
	if err == nil && strings.TrimSpace(string(b)) == id {
		return false, nil
	}
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	if err = config.AtomicWrite(path, []byte(id+"\n"), 0600); err != nil {
		return false, err
	}
	return true, nil
}
