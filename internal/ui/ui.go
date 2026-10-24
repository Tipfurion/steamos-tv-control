package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/Tipfurion/steamos-tv-control/internal/config"
	"github.com/Tipfurion/steamos-tv-control/internal/control"
	"github.com/Tipfurion/steamos-tv-control/internal/ipc"
	"github.com/Tipfurion/steamos-tv-control/internal/service"
	"github.com/Tipfurion/steamos-tv-control/internal/tv"
	atv "github.com/jkiddo/atvremote/pkg/v2/remote"
)

type result struct {
	config  *config.Config
	message string
	err     error
	next    string
	remote  *tv.Remote
	certDir string
	devices []atv.DiscoveredDevice
	status  *control.Status
}
type model struct {
	wizard  bool
	ctx     context.Context
	p       config.Paths
	c       config.Config
	screen  string
	input   textinput.Model
	busy    bool
	message string
	remote  *tv.Remote
	certDir string
	devices []atv.DiscoveredDevice
	status  control.Status
}

func Run(ctx context.Context, p config.Paths) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	c, err := config.Load(p)
	m := &model{ctx: ctx, p: p, c: c}
	m.input = textinput.New()
	m.input.CharLimit = 2048
	m.input.SetWidth(65)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		m.screen = "host"
		m.input.Focus()
	} else {
		m.screen = "menu"
	}
	_, err = tea.NewProgram(m, tea.WithContext(ctx)).Run()
	if m.remote != nil {
		m.remote.Close()
	}
	if m.certDir != "" && m.c.CertDir != m.certDir {
		os.RemoveAll(filepath.Join(p.Data, m.certDir))
	}
	return err
}
func (m *model) Init() tea.Cmd {
	if m.screen == "menu" {
		return m.refresh()
	}
	return textinput.Blink
}
func (m *model) field(screen, value string) {
	m.screen = screen
	m.input.SetValue(value)
	m.input.Focus()
	m.message = ""
}
func (m *model) refresh() tea.Cmd {
	p := m.p
	ctx := m.ctx
	return func() tea.Msg {
		call, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		s, err := ipc.Status(call, p.Socket())
		if err != nil {
			return result{message: "Service is not running. Manual TV controls are available."}
		}
		return result{status: &s}
	}
}
func (m *model) save() tea.Cmd {
	c, p := m.c, m.p
	ctx := m.ctx
	return func() tea.Msg {
		err := config.Save(p, c)
		if err == nil && runtime.GOOS == "linux" {
			call, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			if _, e := ipc.Status(call, p.Socket()); e == nil {
				err = service.Restart()
			}
		}
		return result{message: "Settings saved", err: err, next: "menu"}
	}
}
func (m *model) command(action string) tea.Cmd {
	c, p, ctx := m.c, m.p, m.ctx
	return func() tea.Msg {
		call, cancel := context.WithTimeout(ctx, time.Duration(c.WakeSeconds)*time.Second)
		defer cancel()
		probe, stop := context.WithTimeout(call, 500*time.Millisecond)
		_, err := ipc.Status(probe, p.Socket())
		stop()
		var s control.Status
		if err == nil {
			s, err = ipc.Command(call, p.Socket(), action, c.HDMI)
		} else {
			r := tv.New(c, p)
			defer r.Close()
			ctl := control.New(c, r)
			ctl.ToggleFile = filepath.Join(p.State, "pending-power.json")
			if action == "off" {
				off, end := context.WithTimeout(call, 2*time.Second)
				defer end()
				err = ctl.Execute(off, action, c.HDMI)
			} else {
				err = ctl.Execute(call, action, c.HDMI)
			}
			s = ctl.Status()
		}
		return result{message: "Command completed. Confirm the HDMI picture visually with v.", err: err, status: &s}
	}
}
func (m *model) startPair() tea.Cmd {
	c, p, ctx := m.c, m.p, m.ctx
	return func() tea.Msg {
		if runtime.GOOS == "linux" {
			probe, stop := context.WithTimeout(ctx, time.Second)
			_, err := ipc.Status(probe, p.Socket())
			stop()
			if err == nil {
				if err = service.Stop(); err != nil {
					return result{err: err}
				}
			}
		}
		if err := os.MkdirAll(p.Data, 0700); err != nil {
			return result{err: err}
		}
		dir, err := os.MkdirTemp(p.Data, "identity-")
		if err != nil {
			return result{err: err}
		}
		name := strings.TrimPrefix(dir, p.Data+string(os.PathSeparator))
		c.CertDir = name
		r := tv.New(c, p)
		if err = r.Generate(dir); err == nil {
			call, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			err = r.StartPairing(call)
		}
		if err != nil {
			r.Close()
			os.RemoveAll(dir)
			return result{err: err}
		}
		return result{remote: r, certDir: name, next: "pin", message: "Enter the six-character code shown on the TV"}
	}
}
func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch x := msg.(type) {
	case result:
		m.busy = false
		if x.config != nil {
			m.c = *x.config
			m.wizard = true
			m.certDir = ""
			if m.remote != nil {
				m.remote.Close()
				m.remote = nil
			}
		}
		if x.remote != nil {
			if m.remote != nil {
				m.remote.Close()
			}
			m.remote = x.remote
			m.certDir = x.certDir
		}
		if x.devices != nil {
			m.devices = x.devices
		}
		if x.next != "" {
			m.field(x.next, "")
		}
		m.message = x.message
		if x.err != nil {
			m.message = "Error: " + x.err.Error()
		}
		if x.status != nil {
			m.status = *x.status
		}
		return m, nil
	case tea.KeyPressMsg:
		key := x.String()
		if key == "ctrl+c" || key == "ctrl+d" {
			return m, tea.Quit
		}
		if m.busy {
			return m, nil
		}
		if key == "esc" {
			if m.remote != nil {
				m.remote.Close()
				m.remote = nil
			}
			if m.certDir != "" && m.c.CertDir != m.certDir {
				os.RemoveAll(filepath.Join(m.p.Data, m.certDir))
				m.certDir = ""
			}
			m.wizard = false
			m.screen = "menu"
			m.input.Blur()
			return m, nil
		}
		if m.screen == "menu" {
			switch key {
			case "q":
				return m, tea.Quit
			case "1":
				m.busy = true
				return m, m.command("on")
			case "2":
				m.busy = true
				return m, m.command("off")
			case "3":
				m.busy = true
				return m, m.command("input")
			case "r":
				return m, m.refresh()
			case "e":
				m.field("host", m.c.Host)
			case "h":
				m.field("hdmi", strconv.Itoa(m.c.HDMI))
			case "w":
				m.field("mac", m.c.MAC)
			case "m":
				m.field("sequence", FormatSequence(m.c.Sequence))
			case "d":
				m.c.InputMode = "direct"
				m.c.InputVerified = false
				m.busy = true
				return m, m.save()
			case "v":
				m.field("verify", "")
			case "p":
				m.field("power", m.c.PowerMode)
			case "o":
				m.c.AutoOff = !m.c.AutoOff
				m.busy = true
				return m, m.save()
			case "a":
				m.c.AutoOn = !m.c.AutoOn
				m.busy = true
				return m, m.save()
			case "s":
				m.busy = true
				p := m.p
				return m, func() tea.Msg {
					return result{message: "Service installed. Check it with: steamos-tv-control doctor", err: service.Install(p)}
				}
			}
			return m, nil
		}
		if m.screen == "host" && key == "ctrl+f" {
			m.busy = true
			ctx := m.ctx
			return m, func() tea.Msg {
				d, err := tv.Discover(ctx)
				return result{devices: d, err: err, message: "Choose a device number or enter an address"}
			}
		}
		if key == "enter" {
			value := strings.TrimSpace(m.input.Value())
			switch m.screen {
			case "host":
				if n, err := strconv.Atoi(value); err == nil && n >= 1 && n <= len(m.devices) {
					m.c.Host = m.devices[n-1].IP
					m.c.Name = m.devices[n-1].Name
				} else {
					m.c.Host = value
				}
				if err := m.c.Validate(); err != nil {
					m.message = err.Error()
					return m, nil
				}
				m.busy = true
				return m, m.startPair()
			case "pin":
				if m.remote == nil {
					m.message = "Press e to start pairing again"
					return m, nil
				}
				m.busy = true
				r, ctx, c, p, dir := m.remote, m.ctx, m.c, m.p, m.certDir
				return m, func() tea.Msg {
					call, cancel := context.WithTimeout(ctx, 10*time.Second)
					defer cancel()
					err := r.FinishPairing(call, value)
					if err != nil {
						return result{err: err}
					}
					c.CertDir = dir
					c.InputVerified = false
					err = config.Save(p, c)
					if err != nil {
						return result{err: err}
					}
					return result{config: &c, next: "paired"}
				}
			case "paired":
				m.c.InputVerified = false
				m.field("power", m.c.PowerMode)
				return m, nil
			case "hdmi":
				n, err := strconv.Atoi(value)
				if err != nil || n < 1 || n > 4 {
					m.message = "Enter an HDMI port between 1 and 4"
					return m, nil
				}
				m.c.HDMI = n
				m.c.InputVerified = false
				if m.wizard {
					m.field("mac", m.c.MAC)
					return m, nil
				}
				m.busy = true
				return m, m.save()
			case "mac":
				m.c.MAC = value
				m.field("broadcast", m.c.Broadcast)
				return m, nil
			case "power":
				if value != "discrete" && value != "guarded-power" {
					m.message = "discrete or guarded-power"
					return m, nil
				}
				m.c.PowerMode = value
				if m.wizard {
					m.field("hdmi", strconv.Itoa(m.c.HDMI))
					return m, nil
				}
				m.busy = true
				return m, m.save()
			case "broadcast":
				m.c.Broadcast = value
				m.wizard = false
				m.busy = true
				return m, m.save()
			case "sequence":
				steps, err := ParseSequence(value)
				if err != nil {
					m.message = err.Error()
					return m, nil
				}
				m.c.Sequence = steps
				m.c.InputMode = "sequence"
				m.c.InputVerified = false
				m.busy = true
				return m, m.save()
			case "verify":
				if strings.EqualFold(value, "yes") {
					m.c.InputVerified = true
					m.busy = true
					return m, m.save()
				}
				m.message = "Type yes to confirm"
			}
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}
func (m *model) View() tea.View {
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#78DCE8")).Render("SteamOS TV Control")
	var body string
	if m.screen == "menu" {
		body = fmt.Sprintf("TV: %s   HDMI %d (%s, verified: %t)\nPower: %s (%s)   Result: %s\nAutomatic wake: %t   Automatic standby: %t\n\n1 Wake   2 Standby   3 Select HDMI   r Refresh\nh HDMI   m Input sequence   d Direct input   v Confirm HDMI\nw Wake-on-LAN   p Power profile   o Auto standby   a Auto wake\ne Pair   s Install service (Linux)   q Quit", m.c.Host, m.c.HDMI, m.c.InputMode, m.c.InputVerified, m.status.Power, m.c.PowerMode, m.status.Result, m.c.AutoOn, m.c.AutoOff)
	} else {
		labels := map[string]string{"host": "TV IP address or hostname (Ctrl+F to discover, then enter a device number)", "pin": "Pairing code shown on the TV", "paired": "Pairing saved. Press Enter to choose a power profile", "hdmi": "PC HDMI port: 1-4", "mac": "TV network interface MAC (leave empty to disable Wake-on-LAN)", "broadcast": "Broadcast address and port, for example 192.168.1.255:9", "sequence": "Steps: TV_INPUT@700,DPAD_LEFT@200,DPAD_CENTER@500", "verify": "Test HDMI with 3 from Home and another input.\nIf both tests select the PC input, type yes"}
		labels["power"] = "Mode: discrete (WAKEUP/SLEEP) or guarded-power (for TCL)\nPOWER is sent once based on the current state; unconfirmed repeats are blocked.\nOn TCL, enable higher energy use / Screenless service for remote wake."
		body = labels[m.screen] + "\n\n" + m.input.View()
		if m.screen == "host" {
			for i, d := range m.devices {
				body += fmt.Sprintf("\n%d. %s — %s", i+1, d.Name, d.IP)
			}
		}
	}
	if m.busy {
		body += "\n\nWorking…"
	}
	body += "\n\n" + m.message + "\n\nEsc — menu · Ctrl+C — quit"
	view := tea.NewView(lipgloss.NewStyle().Padding(1, 2).Render(title + "\n\n" + body))
	view.AltScreen = true
	return view
}
func ParseSequence(value string) ([]config.Step, error) {
	var steps []config.Step
	for _, part := range strings.Split(value, ",") {
		v := strings.Split(strings.TrimSpace(part), "@")
		if len(v) != 2 {
			return nil, fmt.Errorf("step format: TV_INPUT@700")
		}
		key := strings.ToUpper(strings.TrimSpace(v[0]))
		delay, err := strconv.Atoi(strings.TrimSpace(v[1]))
		if err != nil || !config.ValidInputKey(key) || delay < 0 || delay > 10000 {
			return nil, fmt.Errorf("invalid step: %s", part)
		}
		steps = append(steps, config.Step{Key: key, DelayMS: delay})
	}
	if len(steps) > 64 {
		return nil, fmt.Errorf("at most 64 steps are allowed")
	}
	return steps, nil
}
func FormatSequence(steps []config.Step) string {
	var out []string
	for _, s := range steps {
		out = append(out, fmt.Sprintf("%s@%d", s.Key, s.DelayMS))
	}
	return strings.Join(out, ",")
}
