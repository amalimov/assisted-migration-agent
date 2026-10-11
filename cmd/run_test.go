package cmd

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/kubev2v/rvtools-analyser/internal/config"
	"github.com/kubev2v/rvtools-analyser/internal/store"
)

func TestRVToolsConfiguration(t *testing.T) {
	cfg := config.NewConfigurationWithOptionsAndDefaults()
	cfg.Agent.DataFolder = t.TempDir()
	cfg.Agent.OpaPoliciesFolder = t.TempDir()
	if err := validateConfiguration(cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Agent.ID == "" {
		t.Fatal("local agent ID was not generated")
	}
	if cfg.Server.HTTPPort != 8000 {
		t.Fatalf("unexpected port %d", cfg.Server.HTTPPort)
	}
	if cfg.Server.ListenAddress != "127.0.0.1" {
		t.Fatalf("unexpected listen address %q", cfg.Server.ListenAddress)
	}
	command := NewRunCommand(cfg)
	if command.Flags().Lookup("server-listen-address") == nil {
		t.Fatal("listen address flag is missing")
	}
	if command.Flags().Lookup("rvtools-mode") != nil || command.Flags().Lookup("console-url") != nil {
		t.Fatal("obsolete modes are still exposed")
	}
}

func TestRejectInvalidListenAddress(t *testing.T) {
	cfg := config.NewConfigurationWithOptionsAndDefaults()
	cfg.Agent.DataFolder = t.TempDir()
	cfg.Agent.OpaPoliciesFolder = t.TempDir()
	cfg.Server.ListenAddress = "not-an-ip"
	if err := validateConfiguration(cfg); err == nil {
		t.Fatal("invalid listen address was accepted")
	}
}

func TestPoolRestoresLatestNanosecondCollection(t *testing.T) {
	folder := t.TempDir()
	cfg := config.NewConfigurationWithOptionsAndDefaults(config.WithAgent(config.Agent{DataFolder: folder}))
	pool, err := initPool(cfg)
	if err != nil {
		t.Fatal(err)
	}
	a, err := pool.NewDatabase("one", filepath.Join(folder, "collection_1760000000000000001.duckdb"), time.Unix(0, 1760000000000000001), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
	if err != nil {
		t.Fatal(err)
	}
	b, err := pool.NewDatabase("two", filepath.Join(folder, "collection_1760000000000000002.duckdb"), time.Unix(0, 1760000000000000002), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	pool.Close()
	restored, err := initPool(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	latest, err := restored.Latest()
	if err != nil {
		t.Fatal(err)
	}
	if latest.CreatedAt.UnixNano() != 1760000000000000002 {
		t.Fatalf("wrong latest: %v", latest.CreatedAt)
	}
}
