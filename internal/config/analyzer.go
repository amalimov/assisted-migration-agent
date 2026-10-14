package config

// AnalyzerConfiguration contains only standalone RVTools Analyzer settings.
type AnalyzerConfiguration struct {
	Server            Server
	DataFolder        string
	OpaPoliciesFolder string
	LogFormat         string
	LogLevel          string
	Version           string
	GitCommit         string
}

func NewAnalyzerConfiguration() *AnalyzerConfiguration {
	return &AnalyzerConfiguration{
		Server: Server{
			HTTPPort:   8000,
			ServerMode: "dev",
		},
		LogFormat: "console",
		LogLevel:  "debug",
		Version:   "v0.0.0",
		GitCommit: "unknown",
	}
}
