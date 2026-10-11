package services_test

import (
	"context"
	"testing"
	"time"

	"github.com/kubev2v/assisted-migration-agent/pkg/work"
	"github.com/kubev2v/rvtools-analyser/internal/models"
	services "github.com/kubev2v/rvtools-analyser/internal/services"
)

type completedWorkFactory struct{}

func (completedWorkFactory) Build() work.WorkBuilder[models.CollectorStatus, models.CollectorResult] {
	return work.NewSliceWorkBuilder([]work.WorkUnit[models.CollectorStatus, models.CollectorResult]{
		{Status: func() models.CollectorStatus { return models.CollectorStatus{State: models.CollectorStateCollected} },
			Work: func(_ context.Context, r models.CollectorResult) (models.CollectorResult, error) {
				r.Completed = true
				return r, nil
			}},
	}, func(context.Context, models.CollectorResult) error { return nil })
}

func TestCollectorCompletes(t *testing.T) {
	collector := services.NewCollectorService(completedWorkFactory{})
	if err := collector.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer collector.Stop()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		status := collector.GetStatus()
		if status.State == models.CollectorStateCollected {
			return
		}
		if status.State == models.CollectorStateError {
			t.Fatal(status.Error)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("collector did not complete")
}
