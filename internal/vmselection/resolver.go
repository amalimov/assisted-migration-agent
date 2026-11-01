package vmselection

import (
	"context"
	"fmt"
	"sort"
	"strings"

	sq "github.com/Masterminds/squirrel"
	"github.com/google/uuid"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

// Resolver resolves selectors only within the requested collection. It never
// filters migration_excluded; estimator eligibility is a separate step.
type Resolver struct {
	pool *store.Pool
}

func NewResolver(pool *store.Pool) *Resolver { return &Resolver{pool: pool} }

func (r *Resolver) LatestCollectionID() (string, error) {
	db, err := r.pool.Latest()
	if err != nil {
		return "", err
	}
	return db.ID, nil
}

func (r *Resolver) Resolve(ctx context.Context, collectionID string, selection VMSelection) ([]models.VM, error) {
	if strings.TrimSpace(collectionID) == "" || collectionID == store.MainDatabaseID {
		return nil, srvErrors.NewResourceNotFoundError("collection", collectionID)
	}
	if err := selection.Validate(); err != nil {
		return nil, err
	}
	db, err := r.pool.Get(collectionID)
	if err != nil {
		if srvErrors.IsResourceNotFoundError(err) {
			return nil, srvErrors.NewResourceNotFoundError("collection", collectionID)
		}
		return nil, err
	}
	st, err := db.Store()
	if err != nil {
		return nil, fmt.Errorf("opening collection %s: %w", collectionID, err)
	}

	include := selection.Include
	ids := make(map[string]struct{}, len(include.VMIDs))
	for _, id := range include.VMIDs {
		ids[id] = struct{}{}
	}
	for _, rawID := range include.GroupIDs {
		groupID, err := uuid.Parse(rawID)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid group ID %q", ErrInvalidSelection, rawID)
		}
		if _, err := st.Group().Get(ctx, groupID); err != nil {
			return nil, err
		}
		matched, err := st.Group().GetMatchedIDs(ctx, groupID)
		if err != nil {
			return nil, err
		}
		for _, id := range matched {
			ids[id] = struct{}{}
		}
	}

	if !include.All && len(ids) == 0 && len(include.Clusters) == 0 {
		return []models.VM{}, nil // Existing empty Groups select no VMs.
	}
	var filter sq.Sqlizer
	if !include.All {
		parts := sq.Or{}
		if len(ids) > 0 {
			values := make([]string, 0, len(ids))
			for id := range ids {
				values = append(values, id)
			}
			sort.Strings(values)
			parts = append(parts, sq.Eq{`v."VM ID"`: values})
		}
		if len(include.Clusters) > 0 {
			parts = append(parts, sq.Eq{`v."Cluster"`: include.Clusters})
		}
		filter = parts
	}
	vms, err := st.VM().ListForEstimation(ctx, filter)
	if err != nil {
		return nil, err
	}

	seenIDs := make(map[string]struct{}, len(vms))
	seenClusters := make(map[string]struct{})
	result := make([]models.VM, 0, len(vms))
	for _, vm := range vms {
		if _, exists := seenIDs[vm.ID]; exists {
			continue
		}
		seenIDs[vm.ID] = struct{}{}
		seenClusters[vm.Cluster] = struct{}{}
		result = append(result, vm)
	}
	for _, id := range include.VMIDs {
		if _, ok := seenIDs[id]; !ok {
			return nil, srvErrors.NewResourceNotFoundError("vm", id)
		}
	}
	for _, cluster := range include.Clusters {
		if _, ok := seenClusters[cluster]; !ok {
			return nil, srvErrors.NewResourceNotFoundError("cluster", cluster)
		}
	}
	return result, nil
}
