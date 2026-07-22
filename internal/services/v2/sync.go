package v2

import (
	"context"
	"fmt"
	"time"

	"github.com/kubev2v/assisted-migration-agent/internal/store"
)

// LabelNew is applied to VMs that appear in a new collection but were absent from the previous one.
const LabelNew = "New"

// systemLabels is the set of labels managed internally that must not be copied across collections.
// Add new entries here to exclude additional labels from the sync.
var systemLabels = map[string]bool{
	LabelNew: true,
}

// SyncAttached runs all cross-DB sync operations on the attached schema inside a single
// transaction. prevSt must already have the new collection database attached under attachAlias
// before calling. All four operations (groups, labels, exclusion flags, new-VM labeling) are
// wrapped in a transaction so that a failure in any step rolls back all prior writes — this
// prevents a partial sync where groups exist in the new collection but have no inventory_data
// (which RefreshGroupInventories would have rebuilt had SyncAttached returned nil).
func SyncAttached(ctx context.Context, prevSt *store.Store2, attachAlias string, now time.Time) error {
	return prevSt.WithTx(ctx, func(txCtx context.Context) error {
		if err := prevSt.Group().CopyToAttached(txCtx, attachAlias, now); err != nil {
			return fmt.Errorf("copying groups: %w", err)
		}
		if err := prevSt.VM().CopyLabelsToAttached(txCtx, attachAlias, systemLabels); err != nil {
			return fmt.Errorf("copying VM labels: %w", err)
		}
		if err := prevSt.VM().CopyMigrationExclusionToAttached(txCtx, attachAlias); err != nil {
			return fmt.Errorf("copying migration exclusion flags: %w", err)
		}
		if err := prevSt.VM().LabelNewVMsInAttached(txCtx, attachAlias, LabelNew); err != nil {
			return fmt.Errorf("labeling new VMs: %w", err)
		}
		return nil
	})
}

// RefreshGroupInventories re-evaluates each group's filter expression against the new
// collection's VMs, rebuilds and persists the group inventory JSON.
// No outbox events are emitted — the collection's inventory-update event (stage 9) covers this.
func RefreshGroupInventories(ctx context.Context, newSt *store.Store2, groupSvc *GroupService) error {
	groups, err := newSt.Group().List(ctx, nil, 0, 0)
	if err != nil {
		return fmt.Errorf("listing groups in new collection: %w", err)
	}
	for _, g := range groups {
		if err := newSt.WithTx(ctx, func(txCtx context.Context) error {
			if err := newSt.Group().RefreshMatches(txCtx, g.ID); err != nil {
				return fmt.Errorf("refreshing matches for group %s: %w", g.ID, err)
			}
			vmIDs, err := newSt.Group().GetMatchedIDs(txCtx, g.ID)
			if err != nil {
				return fmt.Errorf("getting matched IDs for group %s: %w", g.ID, err)
			}
			inv, err := groupSvc.buildGroupInventory(txCtx, vmIDs)
			if err != nil {
				return fmt.Errorf("building inventory for group %s: %w", g.ID, err)
			}
			return newSt.Group().UpdateInventory(txCtx, g.ID, inv)
		}); err != nil {
			return err
		}
	}
	return nil
}
