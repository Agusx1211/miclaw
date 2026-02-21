package sandbox

import "github.com/agusx1211/miclaw/config"

func BuildDockerRunArgs(cfg config.SandboxConfig) []string {
	args := []string{"--network=" + cfg.Network}

	for _, m := range cfg.Mounts {
		mount := "type=bind,source=" + m.Host + ",target=" + m.Container
		if m.Mode == "ro" {
			mount += ",readonly"
		}
		args = append(args, "--mount", mount)
	}
	args = append(args, "--user", "1000:1000", "--restart", "unless-stopped")
	return args
}
