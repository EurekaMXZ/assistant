package domain

import "errors"

var (
	ErrNotFound             = errors.New("not found")
	ErrConflict             = errors.New("conflict")
	ErrUnauthorized         = errors.New("unauthorized")
	ErrForbidden            = errors.New("forbidden")
	ErrInvalidInput         = errors.New("invalid input")
	ErrAuthenticationFailed = errors.New("authentication failed")
	ErrPaymentRequired      = errors.New("payment required")
	ErrStorageQuotaExceeded = errors.New("storage quota exceeded")
)

type typedError struct {
	kind    error
	message string
}

func (e typedError) Error() string {
	if e.message != "" {
		return e.message
	}
	if e.kind != nil {
		return e.kind.Error()
	}
	return "error"
}

func (e typedError) Unwrap() error {
	return e.kind
}

func NewUnauthorizedError(message string) error {
	return typedError{kind: ErrUnauthorized, message: message}
}

func NewForbiddenError(message string) error {
	return typedError{kind: ErrForbidden, message: message}
}

func NewValidationError(message string) error {
	return typedError{kind: ErrInvalidInput, message: message}
}

func NewAuthenticationError(message string) error {
	return typedError{kind: ErrAuthenticationFailed, message: message}
}

func NewConflictError(message string) error {
	return typedError{kind: ErrConflict, message: message}
}

func NewPaymentRequiredError(message string) error {
	return typedError{kind: ErrPaymentRequired, message: message}
}
