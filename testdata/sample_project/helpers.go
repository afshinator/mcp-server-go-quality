package main

func WithVerbose(v bool) Option {
	return func(c *Config) {
		c.Verbose = v
	}
}
