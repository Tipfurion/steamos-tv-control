package service

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/Tipfurion/steamos-tv-control/internal/config"
)

const Unit = config.App + ".service"

func UnitPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "systemd", "user", Unit), nil
}
func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
func supported() error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("systemd service installation requires Linux; TV controls and setup also work on macOS")
	}
	return nil
}
func Install(p config.Paths) error {
	slog.Info("Installing user service")
	if err := supported(); err != nil {
		return err
	}
	c, err := config.Load(p)
	if err != nil {
		return err
	}
	if _, err = os.Stat(filepath.Join(p.Certs(c), "key.pem")); err != nil {
		return fmt.Errorf("complete TV pairing first: %w", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	binary := filepath.Join(home, ".local", "bin", config.App)
	if _, err = os.Stat(binary); err != nil {
		return fmt.Errorf("install binary at %s first: %w", binary, err)
	}
	unitPath, err := UnitPath()
	if err != nil {
		return err
	}
	unit := "[Unit]\nDescription=SteamOS TV Control\n\n[Service]\nType=simple\nExecStart=" + strconv.Quote(strings.ReplaceAll(binary, "%", "%%")) + " daemon\nRestart=on-failure\nRestartSec=5\nTimeoutStopSec=3\nUMask=0077\nSyslogIdentifier=steamos-tv-control\nSyslogLevelPrefix=yes\n\n[Install]\nWantedBy=default.target\n"
	if err = config.AtomicWrite(unitPath, []byte(unit), 0644); err != nil {
		return err
	}
	if err = run("systemctl", "--user", "daemon-reload"); err != nil {
		return err
	}
	uid := strconv.Itoa(os.Getuid())
	linger, _ := exec.Command("loginctl", "show-user", uid, "--property=Linger", "--value").Output()
	if strings.TrimSpace(string(linger)) != "yes" {
		if err = run("loginctl", "enable-linger", uid); err != nil {
			return fmt.Errorf("linger authorization failed; files installed but boot setup incomplete: %w", err)
		}
	}
	return run("systemctl", "--user", "enable", "--now", Unit)
}
func Stop() error {
	if err := supported(); err != nil {
		return err
	}
	return run("systemctl", "--user", "stop", Unit)
}
func Restart() error {
	if err := supported(); err != nil {
		return err
	}
	return run("systemctl", "--user", "restart", Unit)
}
func Remove() error {
	if err := supported(); err != nil {
		return err
	}
	path, err := UnitPath()
	if err != nil {
		return err
	}
	if _, err = os.Stat(path); os.IsNotExist(err) {
		return nil
	}
	if err = run("systemctl", "--user", "disable", "--now", Unit); err != nil {
		return err
	}
	if err = os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return run("systemctl", "--user", "daemon-reload")
}
