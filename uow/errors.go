package uow

import (
	"errors"
	"fmt"
)

var (
	ErrBeginFailed    = errors.New("uow: begin failed")
	ErrCommitFailed   = errors.New("uow: commit failed")
	ErrRollbackFailed = errors.New("uow: rollback failed")
	ErrHookFailed     = errors.New("uow: hook failed")
)

func wrapErr(sentinel, inner error) error {
	return fmt.Errorf("%w: %w", sentinel, inner)
}
