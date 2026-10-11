package services

import (
	"context"

	"github.com/kubev2v/rvtools-analyser/internal/models"
	"github.com/kubev2v/rvtools-analyser/internal/store"
)

type InventoryService struct {
	store *store.Store
}

func NewInventoryService(st *store.Store) *InventoryService {
	srv := &InventoryService{
		store: st,
	}

	return srv
}

// GetInventory retrieves the stored inventory.
func (c *InventoryService) GetInventory(ctx context.Context) (*models.Inventory, error) {
	return c.store.Inventory().Get(ctx)
}
