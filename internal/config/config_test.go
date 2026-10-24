package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAtomicConfigAndPermissions(t *testing.T) {
	p := Paths{Config: filepath.Join(t.TempDir(), "home with spaces", "config")}
	c := Default()
	c.Host = "192.0.2.1"
	if err := Save(p, c); err != nil {
		t.Fatal(err)
	}
	got, err := Load(p)
	if err != nil || got.Host != c.Host {
		t.Fatalf("load: %+v %v", got, err)
	}
	info, _ := os.Stat(p.File())
	if info.Mode().Perm() != 0600 {
		t.Fatal(info.Mode())
	}
	c.HDMI = 7
	if err = Save(p, c); err == nil {
		t.Fatal("accepted invalid HDMI")
	}
	got, err = Load(p)
	if err != nil || got.HDMI != 1 {
		t.Fatal("invalid update replaced valid configuration")
	}
}
func TestRejectUnsafeSequencesAndPaths(t *testing.T) {
	for _, mutate := range []func(*Config){func(c *Config) { c.CertDir = "../../outside" }, func(c *Config) { c.Sequence = []Step{{Key: "POWER"}} }, func(c *Config) { c.MAC = "not a mac" }, func(c *Config) { c.WakeSeconds = 0 }} {
		c := Default()
		c.Host = "tv.local"
		mutate(&c)
		if c.Validate() == nil {
			t.Fatal("invalid configuration accepted", c)
		}
	}
}
