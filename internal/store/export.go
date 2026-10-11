package store

import (
	"context"
	"fmt"
	"slices"
)

// ExportStore runs DuckDB COPY queries for CSV export scopes.
type ExportStore struct {
	db QueryInterceptor
}

func NewExportStore(db QueryInterceptor) *ExportStore {
	return &ExportStore{db: db}
}

// SupportedScopes returns all export scope names.
func (s *ExportStore) SupportedScopes() []string {
	scopes := make([]string, 0, len(exportScopes))
	for scope := range exportScopes {
		scopes = append(scopes, scope)
	}
	slices.Sort(scopes)
	return scopes
}

// IsValidScope reports whether scope is a known export scope.
func (s *ExportStore) IsValidScope(scope string) bool {
	_, ok := exportScopes[scope]
	return ok
}

// ExportSupportedScopes returns all export scope names without a database connection.
func ExportSupportedScopes() []string {
	return (&ExportStore{}).SupportedScopes()
}

// IsExportScope reports whether name is a known export scope without a database connection.
func IsExportScope(name string) bool {
	return (&ExportStore{}).IsValidScope(name)
}

// ScopeFilename returns the CSV filename for a COPY scope.
func (s *ExportStore) ScopeFilename(scope string) (string, bool) {
	spec, ok := exportScopes[scope]
	if !ok {
		return "", false
	}
	return spec.filename, true
}

// CopyScope writes the CSV for scope to path using DuckDB COPY.
func (s *ExportStore) CopyScope(ctx context.Context, scope, path string) error {
	spec, ok := exportScopes[scope]
	if !ok {
		return fmt.Errorf("unknown export scope: %s", scope)
	}
	return s.copyQueryToFile(ctx, spec.query, path, spec.filename)
}

func (s *ExportStore) copyQueryToFile(ctx context.Context, query, path, label string) error {
	if _, err := s.db.ExecContext(ctx, query, path); err != nil {
		return fmt.Errorf("%s CSV generation failed: %w", label, err)
	}
	return nil
}
