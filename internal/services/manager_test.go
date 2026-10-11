package services_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/kubev2v/rvtools-analyser/internal/config"
	"github.com/kubev2v/rvtools-analyser/internal/models"
	services "github.com/kubev2v/rvtools-analyser/internal/services"
	"github.com/kubev2v/rvtools-analyser/internal/store"
)

func TestRVToolsManagerInitializesWithoutConsole(t *testing.T) {
	pool := store.NewPool(time.Minute)
	defer pool.Close()
	db, err := pool.NewDatabase(store.MainDatabaseID, filepath.Join(t.TempDir(), "agent.duckdb"), time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
	if err != nil {
		t.Fatal(err)
	}
	pool.Add(db)
	cfg := config.NewConfigurationWithOptionsAndDefaults(config.WithAgent(config.Agent{DataFolder: t.TempDir()}))
	manager := services.NewServiceManager(services.WithConfig(cfg), services.WithPool(pool))
	if err := manager.Initialize(); err != nil {
		t.Fatal(err)
	}
	if manager.GetCollectorStatus().State != models.CollectorStateReady {
		t.Fatal("collector should be ready")
	}
}
