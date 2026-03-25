package uow

import "context"

// Tx represents an active transaction.
type Tx interface {
	Commit() error
	Rollback() error
}

// TxFactory creates new transactions.
type TxFactory interface {
	Begin(ctx context.Context) (Tx, error)
}

// TxFactoryFunc is a function adapter for [TxFactory].
type TxFactoryFunc func(ctx context.Context) (Tx, error)

func (f TxFactoryFunc) Begin(ctx context.Context) (Tx, error) { return f(ctx) }

// UnitOfWork orchestrates transactional execution.
type UnitOfWork interface {
	Do(ctx context.Context, fn func(tx Tx) error) error
}

// New creates a [UnitOfWork] backed by the given [TxFactory].
func New(factory TxFactory, opts ...Option) UnitOfWork {
	e := &executor{
		factory: factory,
		cfg:     defaultConfig(),
	}
	for _, opt := range opts {
		opt.apply(&e.cfg)
	}
	return e
}
