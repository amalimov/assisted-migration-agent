package store

import (
	"context"
	"database/sql"
	"sync/atomic"
	"time"

	pkgstore "github.com/kubev2v/migration-planner/pkg/store"
)

const (
	dbNameKey string = "db_name"
)

type QueryInterceptor = pkgstore.QueryInterceptor

type Store struct {
	qi         QueryInterceptor
	transactor pkgstore.Transactor
	lastAccess *atomic.Int64
}

func newStore(name string, qi QueryInterceptor, transactor pkgstore.Transactor) *Store {
	lastAccess := &atomic.Int64{}
	return &Store{
		qi:         &usageInterceptor{dbName: name, inner: qi, last: lastAccess},
		lastAccess: lastAccess,
		transactor: transactor,
	}
}

func (s *Store) LastAccess() int64 {
	return s.lastAccess.Load()
}

func (s *Store) Querier() QueryInterceptor {
	return s.qi
}

func (s *Store) VerifyConnection(ctx context.Context) error {
	var result int
	return s.qi.QueryRowContext(ctx, "SELECT 1").Scan(&result)
}

// Checkpoint forces a WAL flush to the main database file.
func (s *Store) Checkpoint(ctx context.Context) error {
	_, err := s.qi.ExecContext(ctx, "FORCE CHECKPOINT")
	return err
}

func (s *Store) Inventory() *InventoryStore   { return NewInventoryStore(s.qi) }
func (s *Store) VM() *VMStore                 { return NewVMStore(s.qi) }
func (s *Store) Group() *GroupStore           { return NewGroupStore(s.qi) }
func (s *Store) Collection() *CollectionStore { return NewCollectionStore(s.qi) }
func (s *Store) Export() *ExportStore         { return NewExportStore(s.qi) }

func (s *Store) WithTx(ctx context.Context, fn func(ctx context.Context) error) error {
	return s.transactor.WithTx(ctx, fn)
}

// usageInterceptor updates the lastAccess at every query for pool to know if it is time to close the unused connection.
// This is **best-effort** because QueryRowContext return a sql.Row that keep connection opened so it might happen
// that the last timestamp don't correspont to the timestamp when the last row.Scan query was made.
// But, the pool has 5min timeout which enough for row.Scan to finish scanning all the rows.
type usageInterceptor struct {
	dbName string
	inner  QueryInterceptor
	last   *atomic.Int64
}

func (u *usageInterceptor) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	ctx = context.WithValue(ctx, dbNameKey, u.dbName) //nolint:staticcheck // string key is intentional — cross-library context value
	u.last.Store(time.Now().UnixNano())
	return u.inner.QueryRowContext(ctx, query, args...)
}

func (u *usageInterceptor) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	ctx = context.WithValue(ctx, dbNameKey, u.dbName) //nolint:staticcheck // string key is intentional — cross-library context value
	u.last.Store(time.Now().UnixNano())
	return u.inner.QueryContext(ctx, query, args...)
}

func (u *usageInterceptor) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	ctx = context.WithValue(ctx, dbNameKey, u.dbName) //nolint:staticcheck // string key is intentional — cross-library context value
	u.last.Store(time.Now().UnixNano())
	return u.inner.ExecContext(ctx, query, args...)
}
