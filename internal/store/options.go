package store

import sq "github.com/Masterminds/squirrel"

// WithCollectionID restricts the VM list to rows belonging to a specific collection.
func WithCollectionID(id int64) ListOption {
	return func(b sq.SelectBuilder) sq.SelectBuilder {
		return b.Where(sq.Eq{`v."collection_id"`: id})
	}
}

// WithOrderBy appends an ORDER BY clause.
// clause must be a compile-time constant SQL fragment (e.g. "finished_at DESC").
// Do not pass user-controlled strings — there is no escaping.
func WithOrderBy(clause string) ListOption {
	return func(b sq.SelectBuilder) sq.SelectBuilder {
		return b.OrderBy(clause)
	}
}

// WithLimit caps the number of rows returned.
func WithLimit(n uint64) ListOption {
	return func(b sq.SelectBuilder) sq.SelectBuilder {
		return b.Limit(n)
	}
}
