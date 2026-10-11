package store

import (
	"context"
	"fmt"
	"strings"

	sq "github.com/Masterminds/squirrel"
	duckdb_models "github.com/kubev2v/migration-planner/pkg/duckdb_parser/models"
	"go.uber.org/zap"

	vmfilter "github.com/kubev2v/assisted-migration-agent/internal/filter"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

type VMStore struct {
	db QueryInterceptor
}

func NewVMStore(db QueryInterceptor) *VMStore {
	return &VMStore{db: db}
}

// FilterOption is a SQL WHERE condition for filtering VMs in the flat filter subquery.
type FilterOption = sq.Sqlizer

// List returns VM summaries with filters, sorting, and pagination.
func (s *VMStore) List(ctx context.Context, filter sq.Sqlizer, opts ...ListOption) ([]models.VirtualMachineSummary, error) {
	builder := vmOutputQuery

	// Apply external filters via subquery (filters reference table aliases in vmFilterSubquery)
	if filter != nil {
		subquery := vmFilterSubquery.Where(filter)

		subSQL, subArgs, err := subquery.ToSql()
		if err != nil {
			return nil, err
		}

		builder = builder.Where(sq.Expr(fmt.Sprintf(`v."VM ID" IN (%s)`, subSQL), subArgs...))
	}

	// Apply options (sort, limit, offset)
	for _, opt := range opts {
		builder = opt(builder)
	}

	query, args, err := builder.ToSql()
	if err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

	var vms []models.VirtualMachineSummary
	for rows.Next() {
		var vm models.VirtualMachineSummary
		var groups StringArray
		var migrationExcluded bool
		var labels StringArray
		err := rows.Scan(
			&vm.ID,
			&vm.Name,
			&vm.PowerState,
			&vm.Cluster,
			&vm.Datacenter,
			&vm.Memory,
			&vm.DiskSize,
			&vm.IssueCount,
			&vm.IsTemplate,
			&vm.IsMigratable,
			&groups,
			&migrationExcluded,
			&labels,
		)
		if err != nil {
			return nil, err
		}
		vm.Groups = groups
		vm.MigrationExcluded = migrationExcluded
		vm.Labels = labels
		vms = append(vms, vm)
	}

	return vms, rows.Err()
}

// Count returns the total number of VMs matching the filters.
func (s *VMStore) Count(ctx context.Context, filter sq.Sqlizer) (int, error) {
	builder := sq.Select("COUNT(*)").From("vinfo v")

	if filter != nil {
		subquery := vmFilterSubquery.Where(filter)
		subSQL, subArgs, err := subquery.ToSql()
		if err != nil {
			return 0, err
		}
		builder = builder.Where(sq.Expr(fmt.Sprintf(`v."VM ID" IN (%s)`, subSQL), subArgs...))
	}

	query, args, err := builder.ToSql()
	if err != nil {
		return 0, err
	}

	var count int
	err = s.db.QueryRowContext(ctx, query, args...).Scan(&count)
	return count, err
}

// Get returns full VM details by ID, from the RVTools workbook.
func (s *VMStore) Get(ctx context.Context, id string) (*models.VM, error) {
	rows, err := s.db.QueryContext(ctx, vmGetQuery, id)
	if err != nil {
		return nil, fmt.Errorf("querying VM %s: %w", id, err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return nil, srvErrors.NewResourceNotFoundError("vm", id)
	}

	var pvm duckdb_models.VM
	var groups StringArray

	if err := rows.Scan(
		&pvm.ID, &pvm.Name, &pvm.Folder, &pvm.Host, &pvm.UUID,
		&pvm.Firmware, &pvm.PowerState, &pvm.ConnectionState,
		&pvm.FaultToleranceEnabled, &pvm.CpuCount, &pvm.MemoryMB,
		&pvm.GuestName, &pvm.GuestNameFromVmwareTools, &pvm.HostName,
		&pvm.IpAddress, &pvm.StorageUsed, &pvm.IsTemplate,
		&pvm.ChangeTrackingEnabled, &pvm.DiskEnableUuid, &pvm.Datacenter,
		&pvm.Cluster, &pvm.HWVersion, &pvm.TotalDiskCapacityMiB,
		&pvm.ProvisionedMiB, &pvm.ResourcePool, &pvm.OsDiskComplexity,
		&pvm.MigrationExcluded, &pvm.Labels, &groups,
		&pvm.CpuHotAddEnabled, &pvm.CpuHotRemoveEnabled, &pvm.CpuSockets,
		&pvm.CoresPerSocket, &pvm.MemoryHotAddEnabled, &pvm.BalloonedMemory,
		&pvm.Disks, &pvm.NICs, &pvm.Networks, &pvm.Concerns,
	); err != nil {
		return nil, fmt.Errorf("scanning VM %s: %w", id, err)
	}

	for i := range pvm.Disks {
		pvm.Disks[i].ChangeTrackingEnabled = pvm.ChangeTrackingEnabled
	}

	result := fromDB(pvm)
	result.Groups = groups

	return &result, nil
}

// GetFilterOptions returns the distinct values available for VM filtering.
func (s *VMStore) GetFilterOptions(ctx context.Context) (models.VMFilterOptions, error) {
	row := s.db.QueryRowContext(ctx, vmFilterOptionsQuery)

	var clusters, datacenters, concernLabels, concernCategories StringArray
	if err := row.Scan(&clusters, &datacenters, &concernLabels, &concernCategories); err != nil {
		return models.VMFilterOptions{}, err
	}

	return models.VMFilterOptions{
		Clusters:          clusters,
		Datacenters:       datacenters,
		ConcernLabels:     concernLabels,
		ConcernCategories: concernCategories,
	}, nil
}

// normalizeCategory validates and normalizes an issue category (case-insensitive).
func normalizeCategory(category, issueID string) string {
	// Valid issue categories (lowercase for case-insensitive comparison)
	var validCategories = map[string]string{
		"critical":    "Critical",
		"warning":     "Warning",
		"information": "Information",
		"advisory":    "Advisory",
		"error":       "Error",
	}

	normalized, ok := validCategories[strings.ToLower(category)]
	if ok {
		return normalized
	}
	zap.S().Named("vm_store").Warnw(
		"Unknown issue category encountered, mapping to 'Other'",
		"category", category,
		"issueID", issueID,
	)
	return "Other"
}

func fromDB(pvm duckdb_models.VM) models.VM {
	issues := make([]models.Issue, 0, len(pvm.Concerns))
	criticalCount := 0
	for _, c := range pvm.Concerns {
		normalizedCategory := normalizeCategory(c.Category, c.Id)
		issues = append(issues, models.Issue{
			ID:          c.Id,
			Label:       c.Label,
			Description: c.Assessment,
			Category:    normalizedCategory,
		})
		if normalizedCategory == "Critical" {
			criticalCount++
		}
	}

	disks := make([]models.Disk, 0, len(pvm.Disks))
	var totalDiskCapacityMiB int64
	for _, d := range pvm.Disks {
		disks = append(disks, models.Disk{
			File:     d.File,
			Capacity: d.Capacity,
			Shared:   d.Shared,
			RDM:      d.RDM,
			Bus:      d.Bus,
			Mode:     d.Mode,
		})
		totalDiskCapacityMiB += d.Capacity
	}

	nics := make([]models.NIC, 0, len(pvm.NICs))
	for i, n := range pvm.NICs {
		nics = append(nics, models.NIC{
			MAC:         n.MAC,
			Network:     n.Network.ID,
			Index:       i,
			IPv4Address: n.IPv4Address,
			IPv6Address: n.IPv6Address,
		})
	}

	return models.VM{
		ID:                    pvm.ID,
		Name:                  pvm.Name,
		UUID:                  pvm.UUID,
		Firmware:              pvm.Firmware,
		PowerState:            pvm.PowerState,
		ConnectionState:       pvm.ConnectionState,
		Host:                  pvm.Host,
		Folder:                pvm.Folder,
		Datacenter:            pvm.Datacenter,
		Cluster:               pvm.Cluster,
		CpuCount:              pvm.CpuCount,
		CoresPerSocket:        pvm.CoresPerSocket,
		MemoryMB:              pvm.MemoryMB,
		GuestName:             pvm.GuestName,
		HostName:              pvm.HostName,
		IPAddress:             pvm.IpAddress,
		DiskSize:              totalDiskCapacityMiB,
		StorageUsed:           int64(pvm.StorageUsed),
		IsTemplate:            pvm.IsTemplate,
		IsMigratable:          criticalCount == 0,
		MigrationExcluded:     pvm.MigrationExcluded,
		FaultToleranceEnabled: pvm.FaultToleranceEnabled,
		Disks:                 disks,
		NICs:                  nics,
		Issues:                issues,
		Labels:                pvm.Labels,
	}
}

// ListOption modifies a SELECT query for sorting/pagination.
type ListOption func(sq.SelectBuilder) sq.SelectBuilder

// SortParam represents a single sort parameter with field name and direction.
type SortParam struct {
	Field string
	Desc  bool
}

// ByFilter applies a raw filter DSL expression.
// Returns nil if the expression is empty or fails to parse.
func ByFilter(expr string) sq.Sqlizer {
	if expr == "" {
		return nil
	}
	sqlizer, err := vmfilter.ParseWithDefaultMap([]byte(expr))
	if err != nil {
		zap.S().Named("vm_store").Warnw("failed to parse filter expression", "expression", expr, "error", err)
	}
	return sqlizer
}

// WithVMIDs filters the output query to only include VMs with the given IDs.
// This bypasses the filter subquery, using pre-computed group match results.
func WithVMIDs(ids []string) ListOption {
	return func(b sq.SelectBuilder) sq.SelectBuilder {
		return b.Where(sq.Eq{`v."VM ID"`: ids})
	}
}

// WithLimit sets the LIMIT clause.
func WithLimit(limit uint64) ListOption {
	return func(b sq.SelectBuilder) sq.SelectBuilder {
		return b.Limit(limit)
	}
}

// WithOffset sets the OFFSET clause.
func WithOffset(offset uint64) ListOption {
	return func(b sq.SelectBuilder) sq.SelectBuilder {
		return b.Offset(offset)
	}
}

// WithDefaultSort applies default sorting by VM ID.
func WithDefaultSort() ListOption {
	return func(b sq.SelectBuilder) sq.SelectBuilder {
		return b.OrderBy("id")
	}
}

// WithSort applies multi-field sorting using output aliases.
func WithSort(sorts []SortParam) ListOption {
	apiFieldToDBColumn := map[string]string{
		"name":         "name",
		"vCenterState": "power_state",
		"cluster":      "cluster",
		"datacenter":   "datacenter",
		"diskSize":     "disk_size",
		"memory":       "memory",
		"issues":       "issue_count",
	}

	return func(b sq.SelectBuilder) sq.SelectBuilder {
		var orderClauses []string
		for _, s := range sorts {
			col, ok := apiFieldToDBColumn[s.Field]
			if !ok {
				continue
			}
			dir := "ASC"
			if s.Desc {
				dir = "DESC"
			}
			clause := col + " " + dir

			orderClauses = append(orderClauses, clause)
		}
		orderClauses = append(orderClauses, "id")
		return b.OrderBy(orderClauses...)
	}
}

// UpdateMigrationExcluded sets the migration_excluded flag for a VM in vinfo table.
func (s *VMStore) UpdateMigrationExcluded(ctx context.Context, vmID string, excluded bool) error {
	query, args, err := sq.Update("vinfo").
		Set(`"migration_excluded"`, excluded).
		Where(sq.Eq{`"VM ID"`: vmID}).
		ToSql()
	if err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return srvErrors.NewResourceNotFoundError("VM", vmID)
	}
	return nil
}

// GetMigrationExcludedStates returns the current migration_excluded state for a list of VMs.
// This is used for rollback purposes in batch update operations.
// Returns an error if any VM is not found.
func (s *VMStore) GetMigrationExcludedStates(ctx context.Context, vmIDs []string) (map[string]bool, error) {
	if len(vmIDs) == 0 {
		return make(map[string]bool), nil
	}

	query, args, err := sq.Select(`"VM ID"`, `"migration_excluded"`).
		From("vinfo").
		Where(sq.Expr(`"VM ID" IN (SELECT unnest(?))`, vmIDs)).
		ToSql()
	if err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

	states := make(map[string]bool)
	for rows.Next() {
		var vmID string
		var excluded bool
		if err := rows.Scan(&vmID, &excluded); err != nil {
			return nil, err
		}
		states[vmID] = excluded
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Validate all VMs were found
	if len(states) != len(vmIDs) {
		missing, err := s.validateVMsExist(ctx, vmIDs)
		if err != nil {
			return nil, err
		}
		if len(missing) > 0 {
			return nil, srvErrors.NewResourceNotFoundError("VM", missing[0])
		}
	}

	return states, nil
}

// UpdateMigrationExcludedBatch sets the migration_excluded flag for multiple VMs in a single UPDATE statement.
// Validates all VMs exist only on failure (lazy validation for performance).
// Returns an error if any VM is not found.
func (s *VMStore) UpdateMigrationExcludedBatch(ctx context.Context, vmIDs []string, excluded bool) error {
	// Deduplicate VM IDs to ensure accurate row count validation
	vmIDs = deduplicateVMIDs(vmIDs)

	if len(vmIDs) == 0 {
		return nil
	}

	query, args, err := sq.Update("vinfo").
		Set(`"migration_excluded"`, excluded).
		Where(sq.Expr(`"VM ID" IN (SELECT unnest(?))`, vmIDs)).
		ToSql()
	if err != nil {
		return err
	}

	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	// Verify all VMs were updated
	if int(rowsAffected) != len(vmIDs) {
		// Only query for missing VMs when we detect a mismatch
		missing, err := s.validateVMsExist(ctx, vmIDs)
		if err != nil {
			return err
		}
		if len(missing) > 0 {
			return srvErrors.NewResourceNotFoundError("VM", missing[0])
		}
		// Edge case: rows affected doesn't match but no VMs are missing
		// This could happen due to concurrent deletion or other race conditions
		return fmt.Errorf("expected to update %d VMs but only updated %d", len(vmIDs), rowsAffected)
	}

	return nil
}

// UpdateLabels sets the labels array for a VM in vinfo table.
func (s *VMStore) UpdateLabels(ctx context.Context, vmID string, labels []string) error {
	if labels == nil {
		labels = []string{}
	}

	// Build JSON array: ['label1', 'label2']
	labelsJSON := "[]"
	if len(labels) > 0 {
		escaped := make([]string, len(labels))
		for i, l := range labels {
			escaped[i] = fmt.Sprintf("'%s'", strings.ReplaceAll(l, "'", "''"))
		}
		labelsJSON = "[" + strings.Join(escaped, ",") + "]"
	}

	query, args, err := sq.Update("vinfo").
		Set(`"labels"`, sq.Expr(labelsJSON)).
		Where(sq.Eq{`"VM ID"`: vmID}).
		ToSql()
	if err != nil {
		return err
	}

	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return srvErrors.NewResourceNotFoundError("VM", vmID)
	}

	return nil
}

// ListLabels returns a map from VM ID to its labels, for all VMs with at least one label.
func (s *VMStore) ListLabels(ctx context.Context) (map[string][]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT "VM ID", CAST("labels" AS VARCHAR[]) FROM vinfo WHERE "labels" != '[]'`)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

	result := make(map[string][]string)
	for rows.Next() {
		var vmID string
		var labels StringArray
		if err := rows.Scan(&vmID, &labels); err != nil {
			return nil, err
		}
		if len(labels) > 0 {
			result[vmID] = labels
		}
	}

	return result, rows.Err()
}

// GetAllLabels returns all distinct labels in use across VMs along with their counts.
// The labels and counts are returned in the same order (sorted alphabetically by label).
func (s *VMStore) GetAllLabels(ctx context.Context) ([]string, []int, error) {
	// Build subquery for unnesting labels
	subquery := sq.Select(`unnest(CAST(v."labels" AS VARCHAR[])) AS label`).
		From("vinfo v").
		Where(sq.NotEq{`v."labels"`: "[]"})

	// Build main query with COUNT grouped by label
	query, args, err := sq.Select("label", "COUNT(*) as count").
		FromSelect(subquery, "labels_unnested").
		Where(sq.NotEq{"label": ""}).
		GroupBy("label").
		OrderBy("label ASC").
		ToSql()
	if err != nil {
		return nil, nil, err
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

	var labels []string
	var counts []int
	for rows.Next() {
		var label string
		var count int
		if err := rows.Scan(&label, &count); err != nil {
			return nil, nil, err
		}
		labels = append(labels, label)
		counts = append(counts, count)
	}

	return labels, counts, rows.Err()
}

// AddLabel adds a label to a VM's labels array (idempotent - no duplicates).
func (s *VMStore) AddLabel(ctx context.Context, vmID string, label string) error {
	// Use DuckDB's list functions to add label without creating duplicates
	// list_append adds the element, list_distinct removes duplicates
	query, args, err := sq.Update("vinfo").
		Set(`"labels"`, sq.Expr("list_distinct(list_append(CAST(\"labels\" AS VARCHAR[]), ?))", label)).
		Where(sq.Eq{`"VM ID"`: vmID}).
		ToSql()
	if err != nil {
		return err
	}

	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return srvErrors.NewResourceNotFoundError("VM", vmID)
	}

	return nil
}

// RemoveLabel removes a label from a VM's labels array (idempotent).
func (s *VMStore) RemoveLabel(ctx context.Context, vmID string, label string) error {
	// Use DuckDB's list_filter with lambda to remove the specific label
	// If label doesn't exist, this is a no-op (idempotent)
	query, args, err := sq.Update("vinfo").
		Set(`"labels"`, sq.Expr("list_filter(CAST(\"labels\" AS VARCHAR[]), x -> x != ?)", label)).
		Where(sq.Eq{`"VM ID"`: vmID}).
		ToSql()
	if err != nil {
		return err
	}

	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return srvErrors.NewResourceNotFoundError("VM", vmID)
	}

	return nil
}

// RemoveLabelGlobally removes a label from all VMs that have it.
func (s *VMStore) RemoveLabelGlobally(ctx context.Context, label string) (int, error) {
	// Update all VMs that have the label
	// WHERE clause with list_contains optimizes to only update relevant VMs
	query, args, err := sq.Update("vinfo").
		Set(`"labels"`, sq.Expr("list_filter(CAST(\"labels\" AS VARCHAR[]), x -> x != ?)", label)).
		Where(sq.Expr("list_contains(CAST(\"labels\" AS VARCHAR[]), ?)", label)).
		ToSql()
	if err != nil {
		return 0, err
	}

	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}

	return int(rows), nil
}

// validateVMsExist checks if all provided VM IDs exist in the database.
// Returns the list of missing VM IDs. If all exist, returns an empty slice.
func (s *VMStore) validateVMsExist(ctx context.Context, vmIDs []string) ([]string, error) {
	if len(vmIDs) == 0 {
		return nil, nil
	}

	// Use unnest in a FROM clause to create a derived table
	query := `
		SELECT t.id
		FROM (SELECT unnest(?) AS id) AS t
		WHERE t.id NOT IN (SELECT "VM ID" FROM vinfo)
	`

	rows, err := s.db.QueryContext(ctx, query, vmIDs)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

	var missing []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		missing = append(missing, id)
	}

	return missing, rows.Err()
}

// AddLabelBatch adds a label to multiple VMs in a single UPDATE statement.
// Validates all VMs exist only on failure (lazy validation for performance).
// Returns an error if any VM is not found.
func (s *VMStore) AddLabelBatch(ctx context.Context, vmIDs []string, label string) error {
	if len(vmIDs) == 0 {
		return nil
	}

	query, args, err := sq.Update("vinfo").
		Set(`"labels"`, sq.Expr("list_distinct(list_append(CAST(\"labels\" AS VARCHAR[]), ?))", label)).
		Where(sq.Expr(`"VM ID" IN (SELECT unnest(?))`, vmIDs)).
		ToSql()
	if err != nil {
		return err
	}

	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	// Verify all VMs were updated
	if int(rowsAffected) != len(vmIDs) {
		// Only query for missing VMs when we detect a mismatch
		missing, err := s.validateVMsExist(ctx, vmIDs)
		if err != nil {
			return err
		}
		if len(missing) > 0 {
			return srvErrors.NewResourceNotFoundError("VM", missing[0])
		}
		// Edge case: rows affected doesn't match but no VMs are missing
		// This could happen due to concurrent deletion or other race conditions
		return fmt.Errorf("expected to update %d VMs but only updated %d", len(vmIDs), rowsAffected)
	}

	return nil
}

// RemoveLabelBatch removes a label from multiple VMs in a single UPDATE statement.
// Validates all VMs exist only on failure (lazy validation for performance).
// Returns an error if any VM is not found.
func (s *VMStore) RemoveLabelBatch(ctx context.Context, vmIDs []string, label string) error {
	if len(vmIDs) == 0 {
		return nil
	}

	query, args, err := sq.Update("vinfo").
		Set(`"labels"`, sq.Expr("list_filter(CAST(\"labels\" AS VARCHAR[]), x -> x != ?)", label)).
		Where(sq.Expr(`"VM ID" IN (SELECT unnest(?))`, vmIDs)).
		ToSql()
	if err != nil {
		return err
	}

	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	// Verify all VMs were updated
	if int(rowsAffected) != len(vmIDs) {
		// Only query for missing VMs when we detect a mismatch
		missing, err := s.validateVMsExist(ctx, vmIDs)
		if err != nil {
			return err
		}
		if len(missing) > 0 {
			return srvErrors.NewResourceNotFoundError("VM", missing[0])
		}
		// Edge case: rows affected doesn't match but no VMs are missing
		// This could happen due to concurrent deletion or other race conditions
		return fmt.Errorf("expected to update %d VMs but only updated %d", len(vmIDs), rowsAffected)
	}

	return nil
}

// buildExclusionArray builds a DuckDB array literal from the keys of a string set.
// Used in list_filter lambda expressions to exclude system-managed labels.
// Values are single-quote-escaped for safe embedding in SQL.
func deduplicateVMIDs(input []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(input))

	for _, id := range input {
		if !seen[id] {
			seen[id] = true
			result = append(result, id)
		}
	}

	return result
}

const listForComparisonQuery = `
SELECT v."VM ID", COALESCE(crit.critical_count, 0) = 0 AS migratable
FROM vinfo v
LEFT JOIN (
    SELECT "VM_ID", COUNT(*) AS critical_count
    FROM concerns
    WHERE "Category" = 'Critical'
    GROUP BY "VM_ID"
) crit ON v."VM ID" = crit."VM_ID"
WHERE v."migration_excluded" IS NOT TRUE
ORDER BY v."VM ID"
`

const countDistinctClustersQuery = `
SELECT COUNT(DISTINCT "Cluster")
FROM vinfo
WHERE "Cluster" IS NOT NULL AND "Cluster" != ''
`

// ListForComparison returns all VM IDs with their migratable status.
// Used by ComparisonService to compute cross-collection set differences.
// A VM is migratable when it has zero Critical concerns.
func (s *VMStore) ListForComparison(ctx context.Context) ([]models.VMComparisonRow, error) {
	rows, err := s.db.QueryContext(ctx, listForComparisonQuery)
	if err != nil {
		return nil, fmt.Errorf("listing VMs for comparison: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var result []models.VMComparisonRow
	for rows.Next() {
		var row models.VMComparisonRow
		if err := rows.Scan(&row.VMID, &row.Migratable); err != nil {
			return nil, fmt.Errorf("scanning VM comparison row: %w", err)
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

// CountDistinctClusters returns the number of distinct non-empty cluster names in the collection.
func (s *VMStore) CountDistinctClusters(ctx context.Context) (int, error) {
	var count int
	if err := s.db.QueryRowContext(ctx, countDistinctClustersQuery).Scan(&count); err != nil {
		return 0, fmt.Errorf("counting distinct clusters: %w", err)
	}
	return count, nil
}
