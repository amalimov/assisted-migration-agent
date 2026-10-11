package errors

import (
	"errors"
	"fmt"
)

type ServiceAlreadyStartedError struct{}

func NewServiceAlreadyStartedError() *ServiceAlreadyStartedError {
	return &ServiceAlreadyStartedError{}
}
func (*ServiceAlreadyStartedError) Error() string { return "service already started" }
func IsServiceAlreadyStartedError(err error) bool {
	var target *ServiceAlreadyStartedError
	return errors.As(err, &target)
}

type ResourceNotFoundError struct{ Kind, ID string }

func NewResourceNotFoundError(kind, id string) *ResourceNotFoundError {
	return &ResourceNotFoundError{Kind: kind, ID: id}
}
func NewInventoryNotFoundError() *ResourceNotFoundError {
	return NewResourceNotFoundError("inventory", "")
}
func (e *ResourceNotFoundError) Error() string {
	if e.ID != "" {
		return fmt.Sprintf("%s '%s' not found", e.Kind, e.ID)
	}
	return fmt.Sprintf("%s not found", e.Kind)
}
func IsResourceNotFoundError(err error) bool {
	var target *ResourceNotFoundError
	return errors.As(err, &target)
}

type DuplicateResourceError struct{ Kind, Field, Value string }

func NewDuplicateResourceError(kind, field, value string) *DuplicateResourceError {
	return &DuplicateResourceError{Kind: kind, Field: field, Value: value}
}
func (e *DuplicateResourceError) Error() string {
	return fmt.Sprintf("%s with %s '%s' already exists", e.Kind, e.Field, e.Value)
}
func IsDuplicateResourceError(err error) bool {
	var target *DuplicateResourceError
	return errors.As(err, &target)
}

type OperationInProgressError struct{ operation string }

func NewOperationInProgressError(operation string) *OperationInProgressError {
	return &OperationInProgressError{operation: operation}
}
func NewCollectionInProgressError() *OperationInProgressError {
	return NewOperationInProgressError("collection")
}
func (e *OperationInProgressError) Error() string {
	return fmt.Sprintf("%s already in progress", e.operation)
}
func IsOperationInProgressError(err error) bool {
	var target *OperationInProgressError
	return errors.As(err, &target)
}

type ValidationError struct{ Message string }

func NewValidationError(message string) *ValidationError { return &ValidationError{Message: message} }
func (e *ValidationError) Error() string                 { return e.Message }
func IsValidationError(err error) bool                   { var target *ValidationError; return errors.As(err, &target) }

type InvalidFilterError struct{ Expression, Reason string }

func NewInvalidFilterError(expression, reason string) *InvalidFilterError {
	return &InvalidFilterError{Expression: expression, Reason: reason}
}
func (e *InvalidFilterError) Error() string {
	if e.Reason != "" {
		return fmt.Sprintf("invalid filter expression '%s': %s", e.Expression, e.Reason)
	}
	return fmt.Sprintf("invalid filter expression '%s'", e.Expression)
}
func IsInvalidFilterError(err error) bool {
	var target *InvalidFilterError
	return errors.As(err, &target)
}
