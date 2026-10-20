package config

import sharedconfig "github.com/kubev2v/assisted-migration-agent/internal/config"

// Configuration contains the settings required by the standalone Analyzer.
type Configuration struct {
	Server            sharedconfig.Server
	DataFolder        string
	OpaPoliciesFolder string
	LogFormat         string
	LogLevel          string
	Version           string
	GitCommit         string
}

func NewConfiguration() *Configuration {
	return &Configuration{
		Server: sharedconfig.Server{
			HTTPPort:   8000,
			ServerMode: "dev",
		},
		LogFormat: "console",
		LogLevel:  "debug",
		Version:   "v0.0.0",
		GitCommit: "unknown",
	}
}
