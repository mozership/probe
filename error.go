package probe

import (
	"fmt"
)

// ErrorType represents different categories of errors
type ErrorType string

const (
	ErrorTypeExecution     ErrorType = "execution"
	ErrorTypeConfiguration ErrorType = "configuration"
	ErrorTypeFile          ErrorType = "file"
)

// ProbeError is the base error type, telling the operation that failed
type ProbeError struct {
	Type      ErrorType
	Operation string
	Message   string
	Cause     error
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
	}
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
