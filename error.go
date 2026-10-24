package probe

import (
	"fmt"
)

// ErrorType represents different categories of errors
type ErrorType string

const (
	ErrorTypeValidation    ErrorType = "validation"
	ErrorTypeExecution     ErrorType = "execution"
	ErrorTypeConfiguration ErrorType = "configuration"
	ErrorTypeNetwork       ErrorType = "network"
	ErrorTypeFile          ErrorType = "file"
	// Deprecated: a step whose action failed keeps the action's error as
	// it is, not wrapped in a ProbeError, so that what the action said is
	// what is shown.
	ErrorTypeAction     ErrorType = "action"
	ErrorTypeDependency ErrorType = "dependency"
)

// ProbeError is the base error type with context information
type ProbeError struct {
	Type      ErrorType
	Operation string
	Message   string
	Cause     error
	Context   map[string]any
}

func (e *ProbeError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s error in %s: %s (caused by: %v)", e.Type, e.Operation, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s error in %s: %s", e.Type, e.Operation, e.Message)
}

func (e *ProbeError) Unwrap() error {
	return e.Cause
}

func (e *ProbeError) Is(target error) bool {
	if t, ok := target.(*ProbeError); ok {
		return e.Type == t.Type
	}
	return false
}

// NewProbeError creates a new ProbeError
func NewProbeError(errorType ErrorType, operation, message string, cause error) *ProbeError {
	return &ProbeError{
		Type:      errorType,
		Operation: operation,
		Message:   message,
		Cause:     cause,
		Context:   make(map[string]any),
	}
}

// WithContext adds context information to the error
func (e *ProbeError) WithContext(key string, value any) *ProbeError {
	e.Context[key] = value
	return e
}

// Convenience functions for creating specific error types

func NewExecutionError(operation, message string, cause error) *ProbeError {
	return NewProbeError(ErrorTypeExecution, operation, message, cause)
}

func NewConfigurationError(operation, message string, cause error) *ProbeError {
	return NewProbeError(ErrorTypeConfiguration, operation, message, cause)
}

func NewFileError(operation, message string, cause error) *ProbeError {
	return NewProbeError(ErrorTypeFile, operation, message, cause)
}

// NewActionError creates a ProbeError of an action.
//
// Deprecated: a step whose action failed keeps the action's error as it
// is, not wrapped in a ProbeError, so that what the action said is what is
// shown.
func NewActionError(operation, message string, cause error) *ProbeError {
	return NewProbeError(ErrorTypeAction, operation, message, cause)
}
