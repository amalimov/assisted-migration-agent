package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	sq "github.com/Masterminds/squirrel"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

type InventoryStore struct {
	db QueryInterceptor
}

func NewInventoryStore(db QueryInterceptor) *InventoryStore {
	return &InventoryStore{db: db}
}

// Save upserts the inventory blob for a collection.
func (s *InventoryStore) Save(ctx context.Context, collectionID int64, data []byte) error {
	query, args, err := sq.Insert("inventory").
		Columns("collection_id", "data", "updated_at").
		Values(collectionID, data, sq.Expr("now()")).
		Suffix("ON CONFLICT (collection_id) DO UPDATE SET data = EXCLUDED.data, updated_at = now()").
		ToSql()
	if err != nil {
		return fmt.Errorf("building inventory upsert: %w", err)
	}
	_, err = s.db.ExecContext(ctx, query, args...)
	return err
}

// GetByCollectionID returns the inventory blob for a specific collection.
func (s *InventoryStore) GetByCollectionID(ctx context.Context, collectionID int64) (*models.Inventory, error) {
	query, args, err := sq.Select("data", "created_at", "updated_at").
		From("inventory").
		Where(sq.Eq{"collection_id": collectionID}).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("building inventory select: %w", err)
	}
	var inv models.Inventory
	err = s.db.QueryRowContext(ctx, query, args...).Scan(&inv.Data, &inv.CreatedAt, &inv.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, srvErrors.NewInventoryNotFoundError()
	}
	if err != nil {
		return nil, fmt.Errorf("scanning inventory: %w", err)
	}
	return &inv, nil
}

// GetActive returns the inventory blob for the currently active collection.
func (s *InventoryStore) GetActive(ctx context.Context, colStore *CollectionStore) (*models.Inventory, error) {
	cols, err := colStore.List(ctx, sq.Eq{"vcenter_id": "default", "active": true})
	if err != nil {
		return nil, fmt.Errorf("finding active collection: %w", err)
	}
	if len(cols) == 0 {
		return nil, srvErrors.NewInventoryNotFoundError()
	}
	return s.GetByCollectionID(ctx, cols[0].ID)
}

// DeleteByCollectionID removes the inventory blob for a collection (used during retention).
func (s *InventoryStore) DeleteByCollectionID(ctx context.Context, collectionID int64) error {
	query, args, err := sq.Delete("inventory").Where(sq.Eq{"collection_id": collectionID}).ToSql()
	if err != nil {
		return fmt.Errorf("building inventory delete: %w", err)
	}
	_, err = s.db.ExecContext(ctx, query, args...)
	return err
}
