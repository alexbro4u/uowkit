package uow

import (
	"context"
	"errors"
	"log/slog"
)

type executor struct {
	factory TxFactory
	cfg     config
}

func (e *executor) Do(ctx context.Context, fn func(tx Tx) error) error {
	var lastErr error
	for attempt := 1; attempt <= e.cfg.maxAttempts; attempt++ {
		lastErr = e.do(ctx, fn, attempt)
		if lastErr == nil {
			return nil
		}
		if attempt == e.cfg.maxAttempts || !e.isRetryable(lastErr) {
			return lastErr
		}
		e.log(ctx, slog.LevelDebug, "retrying transaction",
			slog.Int("attempt", attempt),
			slog.String("error", lastErr.Error()),
		)
	}
	return lastErr
}

func (e *executor) do(
	ctx context.Context,
	fn func(tx Tx) error,
	attempt int,
) error {
	ctx = e.applyBeforeBeginHooks(ctx)

	e.log(ctx, slog.LevelDebug, "beginning transaction", slog.Int("attempt", attempt))

	tx, err := e.factory.Begin(ctx)
	if err != nil {
		return wrapErr(ErrBeginFailed, err)
	}

	var txErr error
	panicked := true
	defer func() {
		if !panicked {
			return
		}
		e.log(ctx, slog.LevelError, "panic detected, rolling back transaction")
		if rbErr := tx.Rollback(); rbErr != nil {
			e.log(ctx, slog.LevelError, "rollback after panic failed",
				slog.String("error", rbErr.Error()),
			)
		}
		e.runAfterRollback(ctx, txErr)
	}()

	if err = fn(tx); err != nil {
		txErr = err
		panicked = false
		return e.rollbackAndLog(ctx, tx, "function returned error, rolling back", err)
	}

	if err = e.runBeforeCommitHooks(ctx, tx); err != nil {
		txErr = err
		panicked = false
		return err
	}

	e.log(ctx, slog.LevelDebug, "committing transaction")
	if err = tx.Commit(); err != nil {
		e.log(ctx, slog.LevelError, "commit failed", slog.String("error", err.Error()))
		panicked = false
		return wrapErr(ErrCommitFailed, err)
	}

	e.log(ctx, slog.LevelDebug, "transaction committed successfully")
	e.runAfterCommit(ctx)
	panicked = false
	return nil
}

func (e *executor) isRetryable(err error) bool {
	if e.cfg.retryOn == nil {
		return false
	}
	return e.cfg.retryOn(err)
}

func (e *executor) runAfterCommit(ctx context.Context) {
	for _, hook := range e.cfg.afterCommit {
		hook(ctx)
	}
}

func (e *executor) runAfterRollback(ctx context.Context, err error) {
	for _, hook := range e.cfg.afterRollback {
		hook(ctx, err)
	}
}

//nolint:fatcontext // Context modification in loop is intentional for chaining hooks
func (e *executor) applyBeforeBeginHooks(ctx context.Context) context.Context {
	result := ctx
	for _, hook := range e.cfg.beforeBegin {
		result = hook(result)
	}
	return result
}

func (e *executor) runBeforeCommitHooks(ctx context.Context, tx Tx) error {
	for _, hook := range e.cfg.beforeCommit {
		if err := hook(ctx, tx); err != nil {
			hookErr := wrapErr(ErrHookFailed, err)
			e.log(ctx, slog.LevelDebug, "before-commit hook failed, rolling back",
				slog.String("error", err.Error()),
			)
			if rbErr := tx.Rollback(); rbErr != nil {
				return errors.Join(hookErr, wrapErr(ErrRollbackFailed, rbErr))
			}
			e.runAfterRollback(ctx, err)
			return hookErr
		}
	}
	return nil
}

func (e *executor) rollbackAndLog(ctx context.Context, tx Tx, msg string, err error) error {
	e.log(ctx, slog.LevelDebug, msg, slog.String("error", err.Error()))
	if rbErr := tx.Rollback(); rbErr != nil {
		e.log(ctx, slog.LevelError, "rollback failed", slog.String("error", rbErr.Error()))
		return errors.Join(err, wrapErr(ErrRollbackFailed, rbErr))
	}
	e.runAfterRollback(ctx, err)
	return err
}

func (e *executor) log(ctx context.Context, level slog.Level, msg string, attrs ...slog.Attr) {
	if e.cfg.logger == nil {
		return
	}
	e.cfg.logger.LogAttrs(ctx, level, msg, attrs...)
}
