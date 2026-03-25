package uow

import (
	"context"
	"log/slog"
)

type config struct {
	maxAttempts   int
	retryOn       func(error) bool
	logger        *slog.Logger
	beforeBegin   []func(ctx context.Context) context.Context
	beforeCommit  []func(ctx context.Context, tx Tx) error
	afterCommit   []func(ctx context.Context)
	afterRollback []func(ctx context.Context, err error)
}

func defaultConfig() config {
	return config{
		maxAttempts: 1,
	}
}

// Option configures a [UnitOfWork] created by [New].
type Option interface {
	apply(*config)
}

type optionFunc func(*config)

func (f optionFunc) apply(c *config) { f(c) }

// WithMaxAttempts sets the maximum number of attempts (default: 1).
func WithMaxAttempts(n int) Option {
	return optionFunc(func(c *config) {
		if n < 1 {
			n = 1
		}
		c.maxAttempts = n
	})
}

// WithRetryOn sets a predicate for retryable errors.
func WithRetryOn(fn func(error) bool) Option {
	return optionFunc(func(c *config) {
		c.retryOn = fn
	})
}

// WithLogger injects a structured logger.
func WithLogger(l *slog.Logger) Option {
	return optionFunc(func(c *config) {
		c.logger = l
	})
}

// WithBeforeBegin registers a hook that runs before transaction begins.
func WithBeforeBegin(fn func(ctx context.Context) context.Context) Option {
	return optionFunc(func(c *config) {
		c.beforeBegin = append(c.beforeBegin, fn)
	})
}

// WithBeforeCommit registers a hook that runs before commit.
func WithBeforeCommit(fn func(ctx context.Context, tx Tx) error) Option {
	return optionFunc(func(c *config) {
		c.beforeCommit = append(c.beforeCommit, fn)
	})
}

// WithAfterCommit registers a hook that runs after commit.
func WithAfterCommit(fn func(ctx context.Context)) Option {
	return optionFunc(func(c *config) {
		c.afterCommit = append(c.afterCommit, fn)
	})
}

// WithAfterRollback registers a hook that runs after rollback.
func WithAfterRollback(fn func(ctx context.Context, err error)) Option {
	return optionFunc(func(c *config) {
		c.afterRollback = append(c.afterRollback, fn)
	})
}
