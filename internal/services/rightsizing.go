package services

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

// RightsizingService stores reports in memory and returns mocked data.
// The real implementation will use the govmomi-based PoC in poc/rightsizing.
type RightsizingService struct {
	mu      sync.RWMutex
	reports map[string]models.RightsizingReport
}

func NewRightsizingService() *RightsizingService {
	return &RightsizingService{
		reports: make(map[string]models.RightsizingReport),
	}
}

func (s *RightsizingService) TriggerCollection(ctx context.Context, params models.RightsizingParams) (*models.RightsizingReport, error) {
	// Apply defaults
	if params.LookbackH <= 0 {
		params.LookbackH = 720 // 30 days
	}
	if params.IntervalID <= 0 {
		params.IntervalID = 7200 // monthly
	}

	now := time.Now().UTC()
	lookback := time.Duration(params.LookbackH) * time.Hour
	intervalDur := time.Duration(params.IntervalID) * time.Second

	expectedSamples := 0
	if intervalDur > 0 {
		expectedSamples = int(lookback / intervalDur)
	}

	report := models.RightsizingReport{
		ID:                  uuid.New().String(),
		VCenter:             params.URL,
		ClusterID:           params.ClusterID,
		WindowStart:         now.Add(-lookback),
		WindowEnd:           now,
		IntervalID:          params.IntervalID,
		ExpectedSampleCount: expectedSamples,
		VMs:                 []models.RightsizingVMReport{},
		CreatedAt:           now,
	}

	s.mu.Lock()
	s.reports[report.ID] = report
	s.mu.Unlock()

	return &report, nil
}

func (s *RightsizingService) ListReports(ctx context.Context) ([]models.RightsizingReport, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	reports := make([]models.RightsizingReport, 0, len(s.reports))
	for _, r := range s.reports {
		reports = append(reports, r)
	}

	// Sort by CreatedAt ascending
	sort.Slice(reports, func(i, j int) bool {
		return reports[i].CreatedAt.Before(reports[j].CreatedAt)
	})

	return reports, nil
}

func (s *RightsizingService) GetReport(ctx context.Context, id string) (*models.RightsizingReport, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	r, ok := s.reports[id]
	if !ok {
		return nil, srvErrors.NewResourceNotFoundError("rightsizing report", id)
	}
	return &r, nil
}
