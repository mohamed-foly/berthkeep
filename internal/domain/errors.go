package domain

import "errors"

// Sentinel errors shared by all packages.
var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("conflict")
	ErrInvalid  = errors.New("invalid input")
	ErrBusy     = errors.New("busy")
)
