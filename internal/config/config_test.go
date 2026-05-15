package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadExample(t *testing.T) {
	path := filepath.Join("..", "..", "deploy", "config.example.yaml")
	if _, err := os.Stat(path); err != nil {
		t.Skip("example config not present")
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DeviceID == "" {
		t.Fatal("device_id empty")
	}
	if cfg.Schedule.Interval.Std() <= 0 {
		t.Fatal("interval invalid")
	}
}
