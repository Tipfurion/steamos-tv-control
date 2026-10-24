package service

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Tipfurion/steamos-tv-control/internal/config"
)

func TestNetworkHookKeepsSocketPathLiteral(t *testing.T) {
	path := filepath.Join(t.TempDir(), "home with ' quotes and $(touch injected)", "control.sock")
	cmd := exec.Command("sh", "-c", "socket="+shellQuote(path)+"; printf '%s' \"$socket\"")
	out, err := cmd.Output()
	if err != nil || string(out) != path {
		t.Fatal(string(out), err)
	}
	body := networkHook(1234, 4321, path)
	if strings.Contains(body, "@UID@") || strings.Contains(body, "@SOCKET@") {
		t.Fatal("unexpanded network hook")
	}
	cmd = exec.Command("sh", "-n")
	cmd.Stdin = strings.NewReader(body)
	if out, err = cmd.CombinedOutput(); err != nil {
		t.Fatal(string(out), err)
	}
}

func TestPrepareNetworkHookUsesUserDataDirectory(t *testing.T) {
	p := config.Paths{Data: filepath.Join(t.TempDir(), "data with spaces"), Runtime: "/run/user/1234/steamos-tv-control"}
	source, target, err := prepareNetworkHook(p)
	if err != nil {
		t.Fatal(err)
	}
	if source != filepath.Join(p.Data, "network-pre-down.sh") || target != networkHookPath() {
		t.Fatal(source, target)
	}
	b, err := os.ReadFile(source)
	if err != nil || string(b) != networkHook(os.Getuid(), os.Getgid(), p.Socket()) {
		t.Fatal(string(b), err)
	}
}
