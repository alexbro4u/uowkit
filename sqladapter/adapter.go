package sqladapter

import (
	"context"
	"database/sql"

	"github.com/alexbro4u/uowkit/uow"
)

// TxFactory implements [uow.TxFactory] for [*sql.DB].
type TxFactory struct {
	db   *sql.DB
	opts *sql.TxOptions
}

// NewTxFactory creates a new [TxFactory].
func NewTxFactory(db *sql.DB, opts *sql.TxOptions) *TxFactory {
	return &TxFactory{db: db, opts: opts}
}

func (f *TxFactory) Begin(ctx context.Context) (uow.Tx, error) {
	sqlTx, err := f.db.BeginTx(ctx, f.opts)
	if err != nil {
		return nil, err
	}
	return &tx{sqlTx: sqlTx}, nil
}

type tx struct {
	sqlTx *sql.Tx
}

func (t *tx) Commit() error   { return t.sqlTx.Commit() }
func (t *tx) Rollback() error { return t.sqlTx.Rollback() }

// Unwrap extracts the underlying [*sql.Tx] from a [uow.Tx].
func Unwrap(t uow.Tx) (*sql.Tx, bool) {
	a, ok := t.(*tx)
	if !ok {
		return nil, false
	}
	return a.sqlTx, true
}
