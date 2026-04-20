// Package errors provides custom error types for the SSP ad server.
// All errors carry structured context and support unwrapping via the
// standard errors.Is / errors.As interface.
package errors

import "fmt"

// ErrorType classifies the category of an error for structured API responses.
type ErrorType string

const (
	// ErrorTypeValidation indicates the request payload failed validation.
	ErrorTypeValidation ErrorType = "VALIDATION_ERROR"
	// ErrorTypeInternal indicates an unexpected server-side failure.
	ErrorTypeInternal ErrorType = "INTERNAL_ERROR"
	// ErrorTypeUnauthorized indicates missing or invalid credentials.
	ErrorTypeUnauthorized ErrorType = "UNAUTHORIZED_ERROR"
	// ErrorTypeTimeout indicates the request exceeded the allowed deadline.
	ErrorTypeTimeout ErrorType = "TIMEOUT_ERROR"
	// ErrorTypeBadRequest indicates a malformed or unprocessable request.
	ErrorTypeBadRequest ErrorType = "BAD_REQUEST"
	// ErrorTypeNotFound indicates a resource was not found.
	ErrorTypeNotFound ErrorType = "NOT_FOUND"
)

// APIError represents a structured error returned to API consumers.
// It includes a machine-readable type, an HTTP status code, a human-readable
// message, and an optional map of per-field validation details.
type APIError struct {
	// Type is the machine-readable error category.
	Type ErrorType `json:"type"`
	// StatusCode is the HTTP status code associated with this error.
	StatusCode int `json:"status_code"`
	// Message is a human-readable description of what went wrong.
	Message string `json:"message"`
	// Details contains per-field validation errors when applicable.
	Details map[string]string `json:"details,omitempty"`
	// Err is the underlying wrapped error (omitted from JSON output).
	Err error `json:"-"`
}

// Error implements the error interface, returning the message string.
func (e *APIError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Err)
	}
	return e.Message
}

// Unwrap returns the underlying error for use with errors.Is and errors.As.
func (e *APIError) Unwrap() error {
	return e.Err
}

// NewValidationError creates an APIError for request validation failures.
// The details map should contain field-level error descriptions keyed by field name.
func NewValidationError(message string, details map[string]string) *APIError {
	return &APIError{
		Type:       ErrorTypeValidation,
		StatusCode: 400,
		Message:    message,
		Details:    details,
	}
}

// NewBadRequestError creates an APIError for malformed requests (e.g. unparseable JSON).
func NewBadRequestError(message string, err error) *APIError {
	return &APIError{
		Type:       ErrorTypeBadRequest,
		StatusCode: 400,
		Message:    message,
		Err:        err,
	}
}

// NewInternalError creates an APIError for unexpected server-side failures.
func NewInternalError(message string, err error) *APIError {
	return &APIError{
		Type:       ErrorTypeInternal,
		StatusCode: 500,
		Message:    message,
		Err:        err,
	}
}

// NewTimeoutError creates an APIError for requests that exceed deadline limits.
func NewTimeoutError(message string) *APIError {
	return &APIError{
		Type:       ErrorTypeTimeout,
		StatusCode: 504,
		Message:    message,
	}
}

// NewUnauthorizedError creates an APIError for authentication failures.
func NewUnauthorizedError(message string) *APIError {
	return &APIError{
		Type:       ErrorTypeUnauthorized,
		StatusCode: 401,
		Message:    message,
	}
}

// NewNotFoundError creates an APIError for resource not found failures.
func NewNotFoundError(message string, err error) *APIError {
	return &APIError{
		Type:       ErrorTypeNotFound,
		StatusCode: 404,
		Message:    message,
		Err:        err,
	}
}
