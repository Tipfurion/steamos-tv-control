package app

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/Tipfurion/steamos-tv-control/internal/automation"
	"github.com/Tipfurion/steamos-tv-control/internal/config"
	"github.com/Tipfurion/steamos-tv-control/internal/control"
	"github.com/Tipfurion/steamos-tv-control/internal/ipc"
	"github.com/Tipfurion/steamos-tv-control/internal/logind"
	"github.com/Tipfurion/steamos-tv-control/internal/onboard"
	"github.com/Tipfurion/steamos-tv-control/internal/service"
	"github.com/Tipfurion/steamos-tv-control/internal/tv"
	"github.com/Tipfurion/steamos-tv-control/internal/ui"
)

var Version = "dev"

func printJSON(v any) error {
	e := json.NewEncoder(os.Stdout)
	e.SetIndent("", "  ")
	return e.Encode(v)
}
func Run(ctx context.Context, args []string) error {
	p, err := config.UserPaths()
	if err != nil {
		return err
	}
	if len(args) == 0 || args[0] == "setup" {
		return ui.Run(ctx, p)
	}
	switch args[0] {
	case "configure":
		fs := flag.NewFlagSet("configure", flag.ContinueOnError)
		host := fs.String("host", "", "TV IP address")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return onboard.Configure(ctx, p, os.Stdin, os.Stdout, onboard.Options{Host: *host})
	case "version", "--version":
		fmt.Println(Version)
		return nil
	case "help", "--help", "-h":
		fmt.Print("SteamOS TV Control\n\n  configure              Automatic first-run configuration\n  setup                  Advanced settings (default)\n  discover               Find Google TV devices\n  pair --host IP         Pair using the TV code\n  tv on|off|input N      Manual TV control\n  status [--json]        Service status\n  doctor [--json]        System and network diagnostics\n  daemon                 Background service (Linux)\n  service install|uninstall\n  uninstall [--purge]    Uninstall\n  version\n")
		return nil
	case "discover":
		d, err := tv.Discover(ctx)
		if err != nil {
			return err
		}
		return printJSON(d)
	case "pair":
		return pair(ctx, p, args[1:])
	case "daemon":
		return daemon(ctx, p)
	case "status":
		s, err := ipc.Status(ctx, p.Socket())
		if err != nil {
			return fmt.Errorf("service unavailable: %w", err)
		}
		return printJSON(s)
	case "tv":
		if len(args) < 2 {
			return errors.New("tv on|off|input <1–4>")
		}
		action := args[1]
		if action != "on" && action != "off" && action != "input" {
			return errors.New("tv on|off|input <1–4>")
		}
		c, err := config.Load(p)
		if err != nil {
			return err
		}
		hdmi := c.HDMI
		if action == "input" {
			if len(args) != 3 {
				return errors.New("tv input <1–4>")
			}
			hdmi, err = strconv.Atoi(args[2])
			if err != nil || hdmi < 1 || hdmi > 4 {
				return errors.New("HDMI must be between 1 and 4")
			}
		}
		call, cancel := context.WithTimeout(ctx, time.Duration(c.WakeSeconds)*time.Second)
		defer cancel()
		if action == "off" {
			call, cancel = context.WithTimeout(call, 2*time.Second)
			defer cancel()
		}
		probe, stop := context.WithTimeout(ctx, 500*time.Millisecond)
		_, active := ipc.Status(probe, p.Socket())
		stop()
		var s control.Status
		if active == nil {
			s, err = ipc.Command(call, p.Socket(), action, hdmi)
		} else {
			r := tv.New(c, p)
			defer r.Close()
			ctl := control.New(c, r)
			ctl.ToggleFile = filepath.Join(p.State, "pending-power.json")
			err = ctl.Execute(call, action, hdmi)
			s = ctl.Status()
		}
		if e := printJSON(s); e != nil {
			return e
		}
		return err
	case "doctor":
		return doctor(ctx, p)
	case "service":
		if len(args) != 2 {
			return errors.New("service install|uninstall")
		}
		switch args[1] {
		case "install":
			return service.Install(p)
		case "uninstall":
			return service.Remove()
		default:
			return errors.New("service install|uninstall")
		}
	case "uninstall":
		if len(args) > 2 || (len(args) == 2 && args[1] != "--purge") {
			return errors.New("uninstall [--purge]")
		}
		if err = service.Remove(); err != nil {
			return err
		}
		if len(args) == 2 {
			for _, dir := range []string{p.Config, p.Data, p.State, p.Runtime} {
				if err = os.RemoveAll(dir); err != nil {
					return err
				}
			}
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		path := filepath.Join(home, ".local", "bin", config.App)
		err = os.Remove(path)
		if os.IsNotExist(err) {
			err = nil
		}
		return err
	default:
		return fmt.Errorf("unknown command %q; use --help", args[0])
	}
}

func pair(ctx context.Context, p config.Paths, args []string) error {
	fs := flag.NewFlagSet("pair", flag.ContinueOnError)
	host := fs.String("host", "", "TV IP address or hostname")
	if err := fs.Parse(args); err != nil {
		return err
	}
	c, err := config.Load(p)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if *host != "" {
		c.Host = *host
	}
	if err = c.Validate(); err != nil {
		return err
	}
	probe, stop := context.WithTimeout(ctx, 500*time.Millisecond)
	_, active := ipc.Status(probe, p.Socket())
	stop()
	if active == nil {
		return errors.New("stop the service before pairing: systemctl --user stop steamos-tv-control")
	}
	if err = os.MkdirAll(p.Data, 0700); err != nil {
		return err
	}
	dir, err := os.MkdirTemp(p.Data, "identity-")
	if err != nil {
		return err
	}
	keep := false
	defer func() {
		if !keep {
			os.RemoveAll(dir)
		}
	}()
	c.CertDir = filepath.Base(dir)
	c.InputVerified = false
	r := tv.New(c, p)
	defer r.Close()
	if err = r.Generate(dir); err != nil {
		return err
	}
	call, cancel := context.WithTimeout(ctx, 10*time.Second)
	err = r.StartPairing(call)
	cancel()
	if err != nil {
		return err
	}
	fmt.Printf("Pairing code shown on TV %s: ", c.Host)
	line := make(chan string, 1)
	go func() {
		scan := bufio.NewScanner(os.Stdin)
		if scan.Scan() {
			line <- scan.Text()
		} else {
			close(line)
		}
	}()
	timer := time.NewTimer(2 * time.Minute)
	defer timer.Stop()
	var pin string
	select {
	case pin = <-line:
	case <-timer.C:
		return errors.New("pairing code entry timed out")
	case <-ctx.Done():
		return ctx.Err()
	}
	call, cancel = context.WithTimeout(ctx, 10*time.Second)
	err = r.FinishPairing(call, pin)
	cancel()
	if err != nil {
		return err
	}
	if err = config.Save(p, c); err != nil {
		return err
	}
	keep = true
	fmt.Println("Pairing saved. Use setup to adjust HDMI and wake settings.")
	return nil
}

func daemon(ctx context.Context, p config.Paths) error {
	if runtime.GOOS != "linux" {
		return errors.New("daemon requires Linux/systemd; discover, pair and tv also work on macOS")
	}
	c, err := config.Load(p)
	if err != nil {
		return err
	}
	slog.Info("Starting daemon", "version", Version, "host", c.Host, "power_mode", c.PowerMode, "auto_on", c.AutoOn, "auto_off", c.AutoOff)
	ln, err := ipc.Listen(p.Socket())
	if err != nil {
		return err
	}
	defer ln.Close()
	m, err := logind.Open("")
	if err != nil {
		return err
	}
	defer m.Close()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	r := tv.New(c, p)
	defer r.Close()
	ctl := control.New(c, r)
	ctl.ToggleFile = filepath.Join(p.State, "pending-power.json")
	events := make(chan automation.Event, 16)
	go automation.Run(ctx, events, c, ctl)
	watchErrors := make(chan error, 1)
	go func() { watchErrors <- m.Watch(ctx, events) }()
	select {
	case <-m.Ready:
	case err = <-watchErrors:
		return err
	case <-ctx.Done():
		return nil
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ctl.Status())
	})
	mux.HandleFunc("POST /command", func(w http.ResponseWriter, req *http.Request) {
		action := req.URL.Query().Get("action")
		if action != "on" && action != "off" && action != "input" {
			http.Error(w, "invalid action", 400)
			return
		}
		hdmi, _ := strconv.Atoi(req.URL.Query().Get("hdmi"))
		slog.Info("Manual TV action requested", "action", action, "hdmi", hdmi)
		duration := time.Duration(c.WakeSeconds) * time.Second
		if action == "off" {
			duration = 2 * time.Second
		}
		call, stop := context.WithTimeout(req.Context(), duration)
		defer stop()
		err := ctl.Execute(call, action, hdmi)
		out := struct {
			Status control.Status `json:"status"`
			Error  string         `json:"error,omitempty"`
		}{Status: ctl.Status()}
		if err != nil {
			out.Error = err.Error()
			w.WriteHeader(http.StatusBadGateway)
		}
		json.NewEncoder(w).Encode(out)
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 3 * time.Second, IdleTimeout: 10 * time.Second, BaseContext: func(net.Listener) context.Context { return ctx }}
	serverErrors := make(chan error, 1)
	go func() { serverErrors <- server.Serve(ln) }()
	defer server.Close()
	id, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return err
	}
	first, err := automation.ClaimBoot(p, string(id))
	if err != nil {
		return err
	}
	if first {
		slog.Info("New PC boot detected; applying startup policy")
		events <- automation.Event{Kind: "boot"}
	} else {
		slog.Info("Service restarted in the same PC boot; automatic wake skipped")
	}
	slog.Info("daemon ready", "host", c.Host, "shutdown_metadata", m.Metadata, "input_verified", c.InputVerified)
	defer slog.Info("Daemon stopped")
	go func() {
		timer := time.NewTicker(15 * time.Second)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				probe, stop := context.WithTimeout(ctx, 3*time.Second)
				ctl.Refresh(probe)
				stop()
			}
		}
	}()
	select {
	case <-ctx.Done():
		return nil
	case err = <-watchErrors:
		return err
	case err = <-serverErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func doctor(ctx context.Context, p config.Paths) error {
	checks := map[string]any{"version": Version, "os": runtime.GOOS, "arch": runtime.GOARCH, "config": p.File(), "socket": p.Socket()}
	c, err := config.Load(p)
	if err != nil {
		checks["configuration_error"] = err.Error()
	} else {
		checks["host"] = c.Host
		checks["input_verified"] = c.InputVerified
		checks["wake_on_lan"] = c.MAC != ""
		for _, port := range []string{"6466", "6467"} {
			dial := net.Dialer{Timeout: 2 * time.Second}
			conn, e := dial.DialContext(ctx, "tcp", net.JoinHostPort(c.Host, port))
			if e == nil {
				conn.Close()
				checks["tcp_"+port] = "reachable"
			} else {
				checks["tcp_"+port] = e.Error()
			}
		}
		_, e := os.Stat(filepath.Join(p.Certs(c), "key.pem"))
		checks["paired_identity_present"] = e == nil
		checks["power_mode"] = c.PowerMode
		if e == nil {
			r := tv.New(c, p)
			call, stop := context.WithTimeout(ctx, 3*time.Second)
			if e = r.Connect(call); e == nil {
				checks["device_info"] = r.Info()
				checks["power"] = r.Power()
			} else {
				checks["remote_error"] = e.Error()
			}
			stop()
			r.Close()
		}
	}
	call, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if s, e := ipc.Status(call, p.Socket()); e == nil {
		checks["daemon"] = s
	} else {
		checks["daemon"] = "not running"
	}
	if runtime.GOOS == "linux" {
		m, e := logind.Open("")
		if e != nil {
			checks["logind_error"] = e.Error()
		} else {
			checks["shutdown_metadata"] = m.Metadata
			checks["sleep_budget_ms"] = m.Budget.Milliseconds()
			m.Close()
		}
		if b, e := os.ReadFile("/etc/os-release"); e == nil {
			checks["os_release"] = strings.TrimSpace(string(b))
		}
	}
	return printJSON(checks)
}
