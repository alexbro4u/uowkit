# uowkit

[![Go Reference](https://pkg.go.dev/badge/github.com/alexbro4u/uowkit.svg)](https://pkg.go.dev/github.com/alexbro4u/uowkit)
[![Go Report Card](https://goreportcard.com/badge/github.com/alexbro4u/uowkit)](https://goreportcard.com/report/github.com/alexbro4u/uowkit)

A lightweight, database-agnostic **Unit of Work** library for Go.

## Features

- **Explicit transaction control** — no hidden context-based transactions
- **Panic safety** — rollback on panic, original panic re-raised
- **Retry mechanism** — configurable attempts with error predicate
- **Lifecycle hooks** — before-begin, before-commit, after-commit, after-rollback
- **Structured logging** — optional `slog` integration
- **Zero dependencies** — standard library only
- **Database-agnostic** — bring your own `TxFactory`; ships with a `database/sql` adapter

## Install

```bash
go get github.com/alexbro4u/uowkit
```

## Quick start

```go
package main

import (
    "context"
    "database/sql"
    "log"

    _ "github.com/lib/pq" // or any driver

    "github.com/alexbro4u/uowkit/sqladapter"
    "github.com/alexbro4u/uowkit/uow"
)

func main() {
    db, err := sql.Open("postgres", "postgres://localhost/mydb?sslmode=disable")
    if err != nil {
        log.Fatal(err)
    }

    factory := sqladapter.NewTxFactory(db, nil)
    u := uow.New(factory)

    err = u.Do(context.Background(), func(tx uow.Tx) error {
        sqlTx, _ := sqladapter.Unwrap(tx)
        _, err := sqlTx.ExecContext(context.Background(), "INSERT INTO users (name) VALUES ($1)", "Alice")
        return err
    })
    if err != nil {
        log.Fatal(err)
    }
}
```

## Core API

```go
// Tx represents an active transaction.
type Tx interface {
    Commit() error
    Rollback() error
}

// TxFactory creates new transactions.
type TxFactory interface {
    Begin(ctx context.Context) (Tx, error)
}

// UnitOfWork orchestrates transactional execution.
type UnitOfWork interface {
    Do(ctx context.Context, fn func(tx Tx) error) error
}
```

Create a `UnitOfWork` with functional options:

```go
u := uow.New(factory,
    uow.WithMaxAttempts(3),
    uow.WithRetryOn(func(err error) bool {
        return errors.Is(err, errDeadlock)
    }),
    uow.WithLogger(slog.Default()),
    uow.WithBeforeBegin(func(ctx context.Context) context.Context {
        return ctx // enrich context, e.g. add trace span
    }),
    uow.WithBeforeCommit(func(ctx context.Context, tx uow.Tx) error {
        return nil // validate before commit; return error to rollback
    }),
    uow.WithAfterCommit(func(ctx context.Context) {
        // post-commit side effects (e.g. publish events)
    }),
    uow.WithAfterRollback(func(ctx context.Context, err error) {
        // observe rollback (e.g. metrics)
    }),
)
```

## Options

| Option | Description |
|---|---|
| `WithMaxAttempts(n)` | Max attempts (≥1). Default: 1 (no retry). |
| `WithRetryOn(fn)` | Predicate deciding if an error is retryable. |
| `WithLogger(l)` | Inject `*slog.Logger` for lifecycle logging. |
| `WithBeforeBegin(fn)` | Hook before `TxFactory.Begin`. Returns enriched context. |
| `WithBeforeCommit(fn)` | Hook before commit. Return error to rollback instead. |
| `WithAfterCommit(fn)` | Hook after successful commit. |
| `WithAfterRollback(fn)` | Hook after rollback with the causing error. |

## Error handling

Sentinel errors support `errors.Is`:

```go
errors.Is(err, uow.ErrBeginFailed)    // transaction start failed
errors.Is(err, uow.ErrCommitFailed)   // commit failed
errors.Is(err, uow.ErrRollbackFailed) // rollback itself failed
errors.Is(err, uow.ErrHookFailed)     // lifecycle hook returned error
```

When a rollback fails alongside the original error, both are joined via `errors.Join` so either can be matched.

## Custom adapter

Implement `uow.TxFactory` for any transactional backend:

```go
type RedisTxFactory struct{ client *redis.Client }

func (f *RedisTxFactory) Begin(ctx context.Context) (uow.Tx, error) {
    pipe := f.client.TxPipeline()
    return &redisTx{pipe: pipe}, nil
}
```

Use `uow.TxFactoryFunc` for quick one-off adapters:

```go
factory := uow.TxFactoryFunc(func(ctx context.Context) (uow.Tx, error) {
    return myTx, nil
})
```

## Nested transactions

The core library intentionally does **not** support nested transactions (savepoints). Reasons:

1. **Savepoints are database-specific** — not all backends support them.
2. **Semantic ambiguity** — a nested rollback that doesn't roll back the outer tx is surprising.
3. **Simplicity** — the flat model is easier to reason about in production.

If you need savepoints, implement them in your `TxFactory` adapter. The `Tx` interface is intentionally minimal to allow this.

## Design for extensions

The library is designed so that **metrics, tracing, and other cross-cutting concerns** can be added without modifying core code:

- **Tracing**: use `WithBeforeBegin` to start a span and `WithAfterCommit`/`WithAfterRollback` to end it.
- **Metrics**: use `WithAfterCommit` and `WithAfterRollback` to increment counters.
- **Middleware**: wrap the `UnitOfWork` interface to add behaviour (e.g. context deadline enforcement).

## License

MIT