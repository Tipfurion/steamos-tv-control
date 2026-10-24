package service

import (
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/Tipfurion/steamos-tv-control/internal/config"
)

//go:embed network-pre-down.sh
var networkHookTemplate string

func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }

func networkHook(uid, gid int, socket string) string {
	return strings.NewReplacer("@UID@", strconv.Itoa(uid), "@GID@", strconv.Itoa(gid), "@SOCKET@", shellQuote(socket)).Replace(networkHookTemplate)
}

func networkHookPath() string {
	return filepath.Join("/etc/NetworkManager/dispatcher.d/pre-down.d", fmt.Sprintf("90-%s-%d", config.App, os.Getuid()))
}

func prepareNetworkHook(p config.Paths) (string, string, error) {
	source := filepath.Join(p.Data, "network-pre-down.sh")
	err := config.AtomicWrite(source, []byte(networkHook(os.Getuid(), os.Getgid(), p.Socket())), 0644)
	return source, networkHookPath(), err
}

func NetworkSleepReady(p config.Paths) bool {
	expected := networkHook(os.Getuid(), os.Getgid(), p.Socket())
	if b, err := os.ReadFile(networkHookPath()); err == nil && string(b) == expected {
		if info, err := os.Stat(networkHookPath()); err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0111 != 0 && info.Mode().Perm()&0022 == 0 {
			if owner, ok := info.Sys().(*syscall.Stat_t); ok && owner.Uid == 0 {
				return true
			}
		}
	}
	return false
}

func installNetworkHook(p config.Paths) error {
	if exec.Command("systemctl", "is-active", "--quiet", "NetworkManager.service").Run() != nil {
		return nil
	}
	if NetworkSleepReady(p) {
		return nil
	}
	for _, path := range []string{"/usr/bin/busctl", "/usr/bin/setpriv", "/usr/bin/curl"} {
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("network sleep integration requires %s: %w", path, err)
		}
	}
	source, target, err := prepareNetworkHook(p)
	if err != nil {
		return err
	}
	fmt.Println("Authorize TV standby before SteamOS disconnects Wi-Fi for sleep.")
	if err = run("sudo", "install", "-D", "-m", "0755", "--", source, target); err != nil {
		return fmt.Errorf("network sleep integration needs authorization; run sudo install -D -m 0755 -- %s %s: %w", shellQuote(source), shellQuote(target), err)
	}
	return nil
}

func removeNetworkHook() error {
	path := networkHookPath()
	if _, err := os.Lstat(path); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	return run("sudo", "rm", "-f", "--", path)
}
