package config

func Default() Config {
	var c Config
	applyDefaults(&c)
	return c
}
