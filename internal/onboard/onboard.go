package onboard

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Tipfurion/steamos-tv-control/internal/config"
	"github.com/Tipfurion/steamos-tv-control/internal/control"
	"github.com/Tipfurion/steamos-tv-control/internal/tv"
	atv "github.com/jkiddo/atvremote/pkg/v2/remote"
)

type Remote interface {
	tv.Device
	Generate(string) error
	StartPairing(context.Context) error
	FinishPairing(context.Context, string) error
	Info() *atv.DeviceInfo
}
type Options struct {
	Host      string
	Discover  func(context.Context) ([]atv.DiscoveredDevice, error)
	NewRemote func(config.Config, config.Paths) Remote
	LearnMAC  func(string) string
}

// Configure uses ordinary terminal prompts; the advanced TUI is never opened.
func Configure(ctx context.Context, p config.Paths, in io.Reader, out io.Writer, opt Options) error {
	if _, err := config.Load(p); err == nil {
		fmt.Fprintln(out, "Your TV is already configured. Use setup for advanced settings.")
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if opt.Discover == nil {
		opt.Discover = tv.Discover
	}
	if opt.NewRemote == nil {
		opt.NewRemote = func(c config.Config, p config.Paths) Remote { return tv.New(c, p) }
	}
	if opt.LearnMAC == nil {
		opt.LearnMAC = tv.NeighborMAC
	}
	scan := bufio.NewScanner(in)
	ask := func(prompt string) (string, error) {
		fmt.Fprint(out, prompt)
		line := make(chan struct {
			v   string
			err error
		}, 1)
		go func() {
			if scan.Scan() {
				line <- struct {
					v   string
					err error
				}{strings.TrimSpace(scan.Text()), nil}
			} else {
				err := scan.Err()
				if err == nil {
					err = io.EOF
				}
				line <- struct {
					v   string
					err error
				}{"", err}
			}
		}()
		select {
		case v := <-line:
			return v.v, v.err
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	c := config.Default()
	c.Host = opt.Host
	if c.Host == "" {
		fmt.Fprintln(out, "Looking for your TV…")
		devices, err := opt.Discover(ctx)
		if err != nil {
			slog.Warn("TV discovery failed", "error", err)
		}
		if len(devices) == 1 {
			c.Host = devices[0].IP
			c.Name = devices[0].Name
			fmt.Fprintf(out, "Found %s (%s).\n", c.Name, c.Host)
		} else {
			for i, d := range devices {
				fmt.Fprintf(out, "  %d. %s (%s)\n", i+1, d.Name, d.IP)
			}
			value, err := ask("TV number or IP address: ")
			if err != nil {
				return err
			}
			if n, e := strconv.Atoi(value); e == nil && n >= 1 && n <= len(devices) {
				c.Host = devices[n-1].IP
				c.Name = devices[n-1].Name
			} else {
				c.Host = value
			}
		}
	}
	if err := c.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(p.Data, 0700); err != nil {
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
	r := opt.NewRemote(c, p)
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
	pairCtx, stop := context.WithTimeout(ctx, 2*time.Minute)
	code, err := askWithContext(pairCtx, ask, "Enter the six-character code shown on your TV: ")
	stop()
	if err != nil {
		return err
	}
	call, cancel = context.WithTimeout(ctx, 10*time.Second)
	err = r.FinishPairing(call, code)
	cancel()
	if err != nil {
		return err
	}
	call, cancel = context.WithTimeout(ctx, 5*time.Second)
	err = r.Connect(call)
	cancel()
	if err != nil {
		return fmt.Errorf("paired, but could not reconnect: %w", err)
	}
	isTCL := false
	if info := r.Info(); info != nil {
		isTCL = strings.Contains(strings.ToLower(info.Manufacturer+" "+info.Model), "tcl")
		if isTCL {
			c.PowerMode = "guarded-power"
		}
		slog.Info("TV identified", "manufacturer", info.Manufacturer, "model", info.Model, "power_mode", c.PowerMode)
	}
	c.MAC = opt.LearnMAC(c.Host)
	// Keep a completed pairing if optional HDMI setup is interrupted.
	if err = config.Save(p, c); err != nil {
		return err
	}
	keep = true
	value, err := ask("PC HDMI port [1-4, Enter to set up later]: ")
	if err != nil {
		return err
	}
	if value != "" {
		n, e := strconv.Atoi(value)
		if e != nil || n < 1 || n > 4 {
			return fmt.Errorf("HDMI must be between 1 and 4")
		}
		c.HDMI = n
		tryInput := func() (bool, error) {
			ctl := control.New(c, r)
			call, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := ctl.Execute(call, "input", n)
			cancel()
			if err != nil {
				if ctx.Err() != nil {
					return false, ctx.Err()
				}
				fmt.Fprintln(out, "The input command could not be sent.")
				slog.Warn("Initial HDMI command failed", "hdmi", n, "input_mode", c.InputMode, "error", err)
				return false, nil
			}
			answer, e := ask("Is the PC picture visible on the TV? [y/N]: ")
			if e != nil {
				return false, e
			}
			return strings.EqualFold(answer, "y") || strings.EqualFold(answer, "yes"), nil
		}
		if c.InputVerified, err = tryInput(); err != nil {
			return err
		}
		if !c.InputVerified && isTCL {
			fmt.Fprintf(out, "Trying another way to select HDMI %d…\n", n)
			// This is a candidate for modern TCL firmware. Input IDs vary
			// between models, so automation still needs visual confirmation.
			c.InputMode = "app-link"
			c.InputURI = "content://android.media.tv/passthrough/com.tcl.tvinput%2F.TvPassThroughService%2FHW" + strconv.Itoa(14+n)
			if c.InputVerified, err = tryInput(); err != nil {
				return err
			}
		}
	}
	if err = config.Save(p, c); err != nil {
		return err
	}
	keep = true
	slog.Info("Initial configuration saved", "host", c.Host, "power_mode", c.PowerMode, "hdmi", c.HDMI, "input_verified", c.InputVerified, "wake_backup", c.MAC != "")
	fmt.Fprintln(out, "TV configured. Automatic power control is ready.")
	if !c.InputVerified {
		fmt.Fprintln(out, "HDMI switching can be enabled later in advanced settings.")
	}
	return nil
}

// The code prompt gets a separate timeout without logging or storing the code.
func askWithContext(ctx context.Context, ask func(string) (string, error), prompt string) (string, error) {
	type answer struct {
		s string
		e error
	}
	ch := make(chan answer, 1)
	go func() { s, e := ask(prompt); ch <- answer{s, e} }()
	select {
	case a := <-ch:
		return a.s, a.e
	case <-ctx.Done():
		return "", ctx.Err()
	}
}
