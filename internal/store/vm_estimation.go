package store

import (
	"context"
	"fmt"

	sq "github.com/Masterminds/squirrel"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
)

// ListForEstimation reads the fields required by Planner's estimators in one
// query. DiskSize is the sum of vdisk capacities in MiB, including zero-disk
// VMs. The caller supplies a vinfo filter or nil to select the full collection.
func (s *VMStore) ListForEstimation(ctx context.Context, filter sq.Sqlizer) ([]models.VM, error) {
	query := sq.Select(
		`v."VM ID"`,
		`COALESCE(v."VM", '')`,
		`COALESCE(v."Cluster", '')`,
		`COALESCE(NULLIF(v."OS according to the VMware Tools", ''), NULLIF(v."OS according to the configuration file", ''), '')`,
		`COALESCE(v."migration_excluded", false)`,
		`COALESCE(v."OsDiskComplexity", 0)`,
		`COALESCE(d.disk_mib, 0)`,
	).From("vinfo v").
		LeftJoin(`(SELECT "VM ID", SUM("Capacity MiB") AS disk_mib FROM vdisk GROUP BY "VM ID") d ON d."VM ID" = v."VM ID"`).
		OrderBy(`v."VM ID"`)
	if filter != nil {
		query = query.Where(filter)
	}
	sqlText, args, err := query.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building estimation VM query: %w", err)
	}
	rows, err := s.db.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return nil, fmt.Errorf("querying estimation VMs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	vms := make([]models.VM, 0)
	for rows.Next() {
		var vm models.VM
		if err := rows.Scan(&vm.ID, &vm.Name, &vm.Cluster, &vm.EffectiveOS,
			&vm.MigrationExcluded, &vm.OsDiskComplexity, &vm.DiskSize); err != nil {
			return nil, fmt.Errorf("scanning estimation VM: %w", err)
		}
		vms = append(vms, vm)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating estimation VMs: %w", err)
	}
	return vms, nil
}
