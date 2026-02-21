package config

import (
	"path/filepath"
	"testing"
)

func TestSaveRoundTrip(t *testing.T) {
	cfg := Default()
	cfg.Provider.Backend = "lmstudio"
	cfg.Provider.Model = "qwen2.5"

	path := filepath.Join(t.TempDir(), "config.json")
	if err := Save(path, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if loaded.Provider.Backend != "lmstudio" || loaded.Provider.Model != "qwen2.5" {
		t.Fatalf("unexpected provider: %#v", loaded.Provider)
	}
}

func TestSaveCreatesParentDirectory(t *testing.T) {
	cfg := Default()
	cfg.Provider.Backend = "lmstudio"
	cfg.Provider.Model = "qwen2.5"

	path := filepath.Join(t.TempDir(), "nested", "config.json")
	if err := Save(path, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	if _, err := Load(path); err != nil {
		t.Fatalf("load saved config: %v", err)
	}
}
