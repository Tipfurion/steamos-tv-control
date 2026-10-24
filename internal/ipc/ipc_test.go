package ipc

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSingleOwnerAndSocketPermissions(t *testing.T) {
	// macOS Unix socket paths are limited to 104 bytes; Go's default test
	// directory includes long per-test names under /var/folders.
	dir, err := os.MkdirTemp("/tmp", "tv-ipc-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	path := filepath.Join(dir, "run", "control.sock")
	ln, err := Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal(info.Mode())
	}
	if other, err := Listen(path); err == nil {
		other.Close()
		t.Fatal("two daemons own socket")
	}
}
func TestDoesNotDeleteForeignFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "control.sock")
	os.WriteFile(path, []byte("keep"), 0600)
	if _, err := Listen(path); err == nil {
		t.Fatal("replaced foreign file")
	}
	b, _ := os.ReadFile(path)
	if string(b) != "keep" {
		t.Fatal("file changed")
	}
}
