package config

type ServerModeType string

const (
	ServerModeProd ServerModeType = "prod"
	ServerModeDev  ServerModeType = "dev"
)

//go:generate go run github.com/ecordell/optgen -output zz_generated.configuration.go . Configuration Server Agent
type Configuration struct {
	Server Server `debugmap:"visible"`
	Agent  Agent  `debugmap:"visible"`

	LogFormat string `debugmap:"visible"`
	LogLevel  string `debugmap:"visible"`
}

type Server struct {
	ServerMode    string `debugmap:"visible" default:"dev"`
	HTTPPort      int    `debugmap:"visible" default:"8000"`
	StaticsFolder string `debugmap:"visible"`
}

type Agent struct {
	ID                string `debugmap:"visible"`
	Version           string `debugmap:"visible" default:"v0.0.0"`
	GitCommit         string `debugmap:"visible" default:"unknown"`
	UIGitCommit       string `debugmap:"visible" default:"unknown"`
	DataFolder        string `debugmap:"visible"`
	OpaPoliciesFolder string `debugmap:"visible"`
}
