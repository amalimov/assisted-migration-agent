package vmselection

import (
	"errors"
	"fmt"
	"strings"
)

var ErrInvalidSelection = errors.New("invalid VM selection")

// VMSelection describes a union of selectors within one collection.
type VMSelection struct {
	Include VMSelectors
}

type VMSelectors struct {
	All      bool
	VMIDs    []string
	GroupIDs []string
	Clusters []string // Existing VM.Cluster values, not a new cluster ID.
}

func (s VMSelection) Validate() error {
	i := s.Include
	if !i.All && len(i.VMIDs) == 0 && len(i.GroupIDs) == 0 && len(i.Clusters) == 0 {
		return fmt.Errorf("%w: include must contain at least one selector", ErrInvalidSelection)
	}
	if i.All && (len(i.VMIDs) > 0 || len(i.GroupIDs) > 0 || len(i.Clusters) > 0) {
		return fmt.Errorf("%w: all cannot be combined with other selectors", ErrInvalidSelection)
	}
	for name, values := range map[string][]string{
		"vms": i.VMIDs, "groups": i.GroupIDs, "clusters": i.Clusters,
	} {
		for _, value := range values {
			if strings.TrimSpace(value) == "" {
				return fmt.Errorf("%w: %s entries must not be empty", ErrInvalidSelection, name)
			}
		}
	}
	return nil
}
