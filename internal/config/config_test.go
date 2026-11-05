package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissingGivesDefaults(t *testing.T) {
	c, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if c.Server.Port != 9221 || c.Ports.RangeStart != 9222 || c.Ports.RangeEnd != 9299 {
		t.Fatalf("unexpected defaults: %+v", c)
	}
}

func TestSaveLoadRoundTripAndPartialFile(t *testing.T) {
	home := t.TempDir()
	c := Default()
	c.Ports.RangeStart, c.Ports.RangeEnd = 9300, 9310
	if err := Save(home, c); err != nil {
		t.Fatal(err)
	}
	got, err := Load(home)
	if err != nil || got.Ports.RangeStart != 9300 {
		t.Fatalf("round trip: %+v %v", got.Ports, err)
	}
	// A partial file keeps defaults for missing fields.
	if err := os.WriteFile(filepath.Join(home, FileName), []byte(`{"stop":{"graceMs":100}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err = Load(home)
	if err != nil || got.Stop.GraceMs != 100 || got.Server.Port != 9221 {
		t.Fatalf("partial: %+v %v", got, err)
	}
}

func TestValidate(t *testing.T) {
	bad := []func(*Config){
		func(c *Config) { c.Server.Host = "0.0.0.0" },
		func(c *Config) { c.Server.Port = 9230 }, // inside CDP range
		func(c *Config) { c.Ports.RangeStart, c.Ports.RangeEnd = 9300, 9200 },
		func(c *Config) { c.Defaults.Mode = "fullscreen" },
	}
	for i, mutate := range bad {
		c := Default()
		mutate(&c)
		if c.Validate() == nil {
			t.Errorf("case %d: expected validation error", i)
		}
	}
	if err := Default().Validate(); err != nil {
		t.Fatalf("defaults invalid: %v", err)
	}
}
