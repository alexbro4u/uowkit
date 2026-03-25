package uow

import (
	"errors"
	"fmt"
)

var (
	// ErrBeginFailed is returned when transaction begin fails.
	ErrBeginFailed = errors.New("uow: begin failed")
	// ErrCommitFailed is returned when transaction commit fails.
	ErrCommitFailed = errors.New("uow: commit failed")
	// ErrRollbackFailed is returned when transaction rollback fails.
	ErrRollbackFailed = errors.New("uow: rollback failed")
	// ErrHookFailed is returned when a hook fails.
	ErrHookFailed = errors.New("uow: hook failed")
)

func wrapErr(sentinel, inner error) error {
	return fmt.Errorf("%w: %w", sentinel, inner)
}
