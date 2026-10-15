package main

import (
	"testing"

	"github.com/kubev2v/assisted-migration-agent/internal/config"
)

func TestAgentNoLongerHasRVToolsModeFlag(t *testing.T) {
	cmd := NewRunCommand(config.NewConfigurationWithOptionsAndDefaults())
	if cmd.Flags().Lookup("rvtools-mode") != nil {
		t.Fatal("temporary RVTools mode flag is still present")
	}
}
