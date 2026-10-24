package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const App = "steamos-tv-control"

type Step struct {
	Key     string `json:"key"`
	DelayMS int    `json:"delay_ms"`
}

type Config struct {
	Version       int    `json:"version"`
	Host          string `json:"host"`
	Name          string `json:"name"`
	HDMI          int    `json:"hdmi"`
	InputMode     string `json:"input_mode"`
	InputURI      string `json:"input_uri,omitempty"`
	Sequence      []Step `json:"sequence,omitempty"`
	InputVerified bool   `json:"input_verified"`
	PowerMode     string `json:"power_mode"`
	AutoOn        bool   `json:"auto_on"`
	AutoOff       bool   `json:"auto_off"`
	MAC           string `json:"mac,omitempty"`
	Broadcast     string `json:"broadcast"`
	WakeSeconds   int    `json:"wake_seconds"`
	CertDir       string `json:"cert_dir,omitempty"`
}

type Paths struct{ Config, Data, State, Runtime string }

func Default() Config {
	return Config{Version: 1, HDMI: 1, InputMode: "direct", PowerMode: "discrete", AutoOn: true, AutoOff: true, Broadcast: "255.255.255.255:9", WakeSeconds: 60}
}

func UserPaths() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, err
	}
	xdg := func(key, fallback string) string {
		if v := os.Getenv(key); filepath.IsAbs(v) {
			return filepath.Join(v, App)
		}
		return filepath.Join(home, fallback, App)
	}
	p := Paths{Config: xdg("XDG_CONFIG_HOME", ".config"), Data: xdg("XDG_DATA_HOME", ".local/share"), State: xdg("XDG_STATE_HOME", ".local/state")}
	if v := os.Getenv("XDG_RUNTIME_DIR"); filepath.IsAbs(v) {
		p.Runtime = filepath.Join(v, App)
	} else {
		p.Runtime = filepath.Join(p.State, "run")
	}
	return p, nil
}
func (p Paths) File() string   { return filepath.Join(p.Config, "config.json") }
func (p Paths) Socket() string { return filepath.Join(p.Runtime, "control.sock") }
func (p Paths) Certs(c Config) string {
	if c.CertDir != "" {
		return filepath.Join(p.Data, c.CertDir)
	}
	return filepath.Join(p.Data, "identity")
}

func Load(p Paths) (Config, error) {
	c := Default()
	data, err := os.ReadFile(p.File())
	if err != nil {
		return c, err
	}
	if err = json.Unmarshal(data, &c); err != nil {
		return c, fmt.Errorf("config: %w", err)
	}
	return c, c.Validate()
}
func (c Config) Validate() error {
	if c.Version != 1 {
		return fmt.Errorf("unsupported configuration version: %d", c.Version)
	}
	if strings.TrimSpace(c.Host) == "" || strings.ContainsAny(c.Host, "/\n\r\t ") {
		return errors.New("enter a TV IP address or hostname without a port")
	}
	if strings.Contains(c.Host, ":") && net.ParseIP(c.Host) == nil {
		return errors.New("enter the TV address without a port or square brackets")
	}
	if c.HDMI < 1 || c.HDMI > 4 {
		return errors.New("HDMI must be between 1 and 4")
	}
	if c.PowerMode != "discrete" && c.PowerMode != "guarded-power" {
		return errors.New("power_mode: discrete or guarded-power")
	}
	if c.WakeSeconds < 1 || c.WakeSeconds > 300 {
		return errors.New("wake_seconds must be between 1 and 300")
	}
	if c.InputMode != "direct" && c.InputMode != "sequence" && c.InputMode != "app-link" {
		return errors.New("input_mode: direct, app-link or sequence")
	}
	if c.InputMode == "app-link" && !ValidInputURI(c.InputURI) {
		return errors.New("input_uri must be an Android TV passthrough content URI")
	}
	if c.InputMode == "sequence" && len(c.Sequence) == 0 {
		return errors.New("empty HDMI sequence")
	}
	if len(c.Sequence) > 64 {
		return errors.New("at most 64 HDMI steps are allowed")
	}
	for _, s := range c.Sequence {
		if !ValidInputKey(s.Key) || s.DelayMS < 0 || s.DelayMS > 10000 {
			return fmt.Errorf("invalid HDMI step: %q", s.Key)
		}
	}
	if c.CertDir != "" && (filepath.IsAbs(c.CertDir) || filepath.Base(c.CertDir) != c.CertDir || c.CertDir == ".." || c.CertDir == ".") {
		return errors.New("invalid certificate directory")
	}
	if c.MAC != "" {
		m, err := net.ParseMAC(c.MAC)
		if err != nil || len(m) != 6 {
			return errors.New("a six-byte network interface MAC address is required")
		}
	}
	host, port, err := net.SplitHostPort(c.Broadcast)
	if err != nil {
		return fmt.Errorf("broadcast: %w", err)
	}
	ip := net.ParseIP(host)
	n, err := strconv.Atoi(port)
	if ip == nil || ip.To4() == nil || err != nil || n < 1 || n > 65535 {
		return errors.New("broadcast: an IPv4 address and port are required, for example 192.168.1.255:9")
	}
	return nil
}

func ValidInputURI(value string) bool {
	u, err := url.Parse(value)
	return err == nil && len(value) <= 2048 && !strings.ContainsAny(value, " \t\r\n") &&
		u.Scheme == "content" && u.Host == "android.media.tv" && u.User == nil &&
		u.RawQuery == "" && u.Fragment == "" &&
		strings.HasPrefix(u.Path, "/passthrough/") && len(strings.TrimPrefix(u.Path, "/passthrough/")) > 0
}

func ValidInputKey(s string) bool {
	s = strings.TrimPrefix(s, "KEYCODE_")
	switch s {
	case "HOME", "BACK", "TV_INPUT", "DPAD_UP", "DPAD_DOWN", "DPAD_LEFT", "DPAD_RIGHT", "DPAD_CENTER", "ENTER":
		return true
	}
	return false
}
func Save(p Paths, c Config) error {
	if err := c.Validate(); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return AtomicWrite(p.File(), append(b, '\n'), 0600)
}

func AtomicWrite(path string, b []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(mode); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
