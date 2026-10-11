package errors_test

import (
	"fmt"
	errors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
	"testing"
)

func TestLocalErrorsAreTypedThroughWrapping(t *testing.T) {
	cases := []struct {
		name  string
		err   error
		check func(error) bool
	}{
		{"missing", errors.NewResourceNotFoundError("vm", "vm-1"), errors.IsResourceNotFoundError},
		{"duplicate", errors.NewDuplicateResourceError("group", "name", "test"), errors.IsDuplicateResourceError},
		{"collecting", errors.NewCollectionInProgressError(), errors.IsOperationInProgressError},
		{"validation", errors.NewValidationError("invalid name"), errors.IsValidationError},
		{"filter", errors.NewInvalidFilterError("bad", "syntax"), errors.IsInvalidFilterError},
		{"work", errors.NewServiceAlreadyStartedError(), errors.IsServiceAlreadyStartedError},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if !c.check(fmt.Errorf("wrapped: %w", c.err)) {
				t.Fatal("typed error not recognized")
			}
		})
	}
}
