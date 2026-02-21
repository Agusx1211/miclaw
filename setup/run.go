package setup

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/agusx1211/miclaw/config"
)

func Run(configPath string, in io.Reader, out io.Writer) error {
	cfg, err := loadConfig(configPath)
	if err != nil {
		return err
	}
	u := newUI(in, out)
	u.section("Miclaw Setup")
	u.note("Press Enter to keep the current value.")
	if err := configurePaths(u, &cfg); err != nil {
		return err
	}
	if err := configureProvider(u, &cfg.Provider); err != nil {
		return err
	}
	if err := configureSignal(u, &cfg.Signal); err != nil {
		return err
	}
	if err := configureSandbox(u, &cfg.Sandbox); err != nil {
		return err
	}
	if err := configureMemory(u, &cfg.Memory); err != nil {
		return err
	}
	if err := configureWebhook(u, &cfg.Webhook); err != nil {
		return err
	}
	save, err := u.askBool("Save configuration now", true)
	if err != nil {
		return err
	}
	if !save {
		u.note("Setup cancelled. No files were changed.")
		return nil
	}
	if err := config.Save(configPath, cfg); err != nil {
		return err
	}
	if _, err := config.Load(configPath); err != nil {
		return err
	}
	fmt.Fprintf(out, "Saved config to %s\n", configPath)
	return nil
}

func loadConfig(configPath string) (config.Config, error) {
	cfg := config.Default()
	existing, err := config.Load(configPath)
	if err == nil {
		return *existing, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	return cfg, err
}
