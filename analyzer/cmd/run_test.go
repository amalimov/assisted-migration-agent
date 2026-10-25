package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kubev2v/assisted-migration-agent/internal/config"
)

func TestAnalyzerCommandHasIndependentFlags(t *testing.T) {
	root := NewRootCommand("test", "test")
	run, _, err := root.Find([]string{"run"})
	if err != nil {
		t.Fatal(err)
	}
	for _, flag := range []string{"data-folder", "opa-policies-folder", "server-http-port", "server-statics-folder"} {
		if run.Flags().Lookup(flag) == nil {
			t.Errorf("analyzer run command is missing %q", flag)
		}
	}
	for _, flag := range []string{"rvtools-mode", "agent-id", "source-id", "console-url"} {
		if run.Flags().Lookup(flag) != nil || run.InheritedFlags().Lookup(flag) != nil {
			t.Errorf("analyzer run command exposes Agent flag %q", flag)
		}
	}
}

func TestAnalyzerConfigurationRequiresLocalInputs(t *testing.T) {
	cfg := config.NewConfigurationWithOptionsAndDefaults()
	if err := validateConfiguration(cfg); err == nil || !strings.Contains(err.Error(), "data folder") {
		t.Fatalf("missing data folder: got %v", err)
	}
	cfg.Agent.DataFolder = t.TempDir()
	if err := validateConfiguration(cfg); err == nil || !strings.Contains(err.Error(), "opa policies") {
		t.Fatalf("missing policy folder: got %v", err)
	}
	cfg.Agent.OpaPoliciesFolder = t.TempDir()
	if err := validateConfiguration(cfg); err != nil {
		t.Fatalf("valid analyzer configuration: %v", err)
	}
}

func TestAnalyzerDataFolderMustExistAndBeDirectory(t *testing.T) {
	cfg := config.NewConfigurationWithOptionsAndDefaults()
	cfg.Agent.OpaPoliciesFolder = t.TempDir()
	parent := t.TempDir()
	cfg.Agent.DataFolder = filepath.Join(parent, "missing")
	if err := validateConfiguration(cfg); err == nil || !strings.Contains(err.Error(), "invalid data folder") {
		t.Fatalf("missing data folder: got %v", err)
	}

	file := filepath.Join(parent, "data-file")
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	cfg.Agent.DataFolder = file
	if err := validateConfiguration(cfg); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("file as data folder: got %v", err)
	}
}

func TestAnalyzerProductionRequiresStaticsFolder(t *testing.T) {
	cfg := config.NewConfigurationWithOptionsAndDefaults()
	cfg.Agent.DataFolder = t.TempDir()
	cfg.Agent.OpaPoliciesFolder = t.TempDir()
	cfg.Server.ServerMode = string(config.ServerModeProd)

	if err := validateConfiguration(cfg); err == nil || !strings.Contains(err.Error(), "statics folder must be set") {
		t.Fatalf("production mode without a static folder: got %v", err)
	}

	cfg.Server.StaticsFolder = t.TempDir()
	if err := validateConfiguration(cfg); err != nil {
		t.Fatalf("production mode with a static folder: %v", err)
	}
}
