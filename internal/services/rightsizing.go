package services

import (
	"context"
	"fmt"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
)

// RightsizingService provides API access to stored rightsizing reports.
type RightsizingService struct {
	store *store.Store
}

func NewRightsizingService(st *store.Store) *RightsizingService {
	return &RightsizingService{store: st}
}

// ListReports returns metadata for all rightsizing reports (no VM metrics).
func (s *RightsizingService) ListReports(ctx context.Context) ([]models.RightsizingReportSummary, error) {
	return s.store.RightSizing().ListReports(ctx)
}

// GetReport returns a single rightsizing report by ID.
// Returns a ResourceNotFoundError if the ID does not exist.
func (s *RightsizingService) GetReport(ctx context.Context, id string) (*models.RightsizingReport, error) {
	return s.store.RightSizing().GetReport(ctx, id)
}

// TriggerCollection is not yet implemented.
func (s *RightsizingService) TriggerCollection(ctx context.Context, params models.RightsizingParams) (*models.RightsizingReport, error) {
	return nil, fmt.Errorf("rightsizing collection trigger is not yet implemented")
}
