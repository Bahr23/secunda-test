package entity

import "errors"

var (
	ErrInvalid         = errors.New("invalid input")
	ErrUnauthenticated = errors.New("invalid credentials")
	ErrForbidden       = errors.New("operation is not permitted")
	ErrNotFound        = errors.New("resource not found")
	ErrConflict        = errors.New("resource conflict")
)
