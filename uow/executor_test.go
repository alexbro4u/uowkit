package uow_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/alexbro4u/uowkit/uow"
)

// --- test doubles ---

type mockTx struct {
	commitErr   error
	rollbackErr error
	committed   bool
	rolledBack  bool
}

func (m *mockTx) Commit() error {
	m.committed = true
	return m.commitErr
}

func (m *mockTx) Rollback() error {
	m.rolledBack = true
	return m.rollbackErr
}

type mockFactory struct {
	tx       *mockTx
	beginErr error
	calls    int
}

func (f *mockFactory) Begin(_ context.Context) (uow.Tx, error) {
	f.calls++
	if f.beginErr != nil {
		return nil, f.beginErr
	}
	// Return a fresh mockTx for each call so retries get independent state.
	if f.calls > 1 {
		return &mockTx{commitErr: f.tx.commitErr, rollbackErr: f.tx.rollbackErr}, nil
	}
	return f.tx, nil
}

// --- tests ---

func TestDo_CommitOnSuccess(t *testing.T) {
	tx := &mockTx{}
	f := &mockFactory{tx: tx}
	u := uow.New(f)

	err := u.Do(context.Background(), func(t uow.Tx) error {
		return nil
	})

	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !tx.committed {
		t.Fatal("expected transaction to be committed")
	}
	if tx.rolledBack {
		t.Fatal("expected transaction NOT to be rolled back")
	}
}

func TestDo_RollbackOnError(t *testing.T) {
	tx := &mockTx{}
	f := &mockFactory{tx: tx}
	u := uow.New(f)
	bizErr := errors.New("business error")

	err := u.Do(context.Background(), func(t uow.Tx) error {
		return bizErr
	})

	if !errors.Is(err, bizErr) {
		t.Fatalf("expected business error, got %v", err)
	}
	if tx.committed {
		t.Fatal("expected transaction NOT to be committed")
	}
	if !tx.rolledBack {
		t.Fatal("expected transaction to be rolled back")
	}
}

func TestDo_RollbackOnPanic(t *testing.T) {
	tx := &mockTx{}
	f := &mockFactory{tx: tx}
	u := uow.New(f)

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic to propagate")
		}
		if r != "boom" {
			t.Fatalf("expected panic value 'boom', got %v", r)
		}
		if !tx.rolledBack {
			t.Fatal("expected transaction to be rolled back after panic")
		}
		if tx.committed {
			t.Fatal("expected transaction NOT to be committed after panic")
		}
	}()

	_ = u.Do(context.Background(), func(t uow.Tx) error {
		panic("boom")
	})
}

func TestDo_BeginError(t *testing.T) {
	f := &mockFactory{beginErr: errors.New("conn refused")}
	u := uow.New(f)

	err := u.Do(context.Background(), func(t uow.Tx) error {
		return nil
	})

	if !errors.Is(err, uow.ErrBeginFailed) {
		t.Fatalf("expected ErrBeginFailed, got %v", err)
	}
}

func TestDo_CommitError(t *testing.T) {
	tx := &mockTx{commitErr: errors.New("disk full")}
	f := &mockFactory{tx: tx}
	u := uow.New(f)

	err := u.Do(context.Background(), func(t uow.Tx) error {
		return nil
	})

	if !errors.Is(err, uow.ErrCommitFailed) {
		t.Fatalf("expected ErrCommitFailed, got %v", err)
	}
}

func TestDo_RollbackError(t *testing.T) {
	tx := &mockTx{rollbackErr: errors.New("rb fail")}
	f := &mockFactory{tx: tx}
	u := uow.New(f)
	bizErr := errors.New("business error")

	err := u.Do(context.Background(), func(t uow.Tx) error {
		return bizErr
	})

	if !errors.Is(err, bizErr) {
		t.Fatalf("expected original error, got %v", err)
	}
	if !errors.Is(err, uow.ErrRollbackFailed) {
		t.Fatalf("expected ErrRollbackFailed, got %v", err)
	}
}

func TestDo_Retry(t *testing.T) {
	attempts := 0
	retryableErr := errors.New("deadlock")

	f := &mockFactory{tx: &mockTx{}}
	u := uow.New(f,
		uow.WithMaxAttempts(3),
		uow.WithRetryOn(func(err error) bool {
			return errors.Is(err, retryableErr)
		}),
	)

	err := u.Do(context.Background(), func(t uow.Tx) error {
		attempts++
		if attempts < 3 {
			return retryableErr
		}
		return nil
	})

	if err != nil {
		t.Fatalf("expected nil after retries, got %v", err)
	}
	if attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts)
	}
}

func TestDo_RetryExhausted(t *testing.T) {
	retryableErr := errors.New("deadlock")
	f := &mockFactory{tx: &mockTx{}}
	u := uow.New(f,
		uow.WithMaxAttempts(2),
		uow.WithRetryOn(func(err error) bool {
			return errors.Is(err, retryableErr)
		}),
	)

	err := u.Do(context.Background(), func(t uow.Tx) error {
		return retryableErr
	})

	if !errors.Is(err, retryableErr) {
		t.Fatalf("expected retryable error after exhaustion, got %v", err)
	}
}

func TestDo_NoRetryOnNonRetryableError(t *testing.T) {
	attempts := 0
	f := &mockFactory{tx: &mockTx{}}
	u := uow.New(f,
		uow.WithMaxAttempts(3),
		uow.WithRetryOn(func(err error) bool { return false }),
	)

	err := u.Do(context.Background(), func(t uow.Tx) error {
		attempts++
		return errors.New("permanent")
	})

	if err == nil {
		t.Fatal("expected error")
	}
	if attempts != 1 {
		t.Fatalf("expected 1 attempt, got %d", attempts)
	}
}

func TestDo_BeforeBeginHook(t *testing.T) {
	type ctxKey string
	f := &mockFactory{tx: &mockTx{}}
	u := uow.New(f,
		uow.WithBeforeBegin(func(ctx context.Context) context.Context {
			return context.WithValue(ctx, ctxKey("key"), "value")
		}),
	)

	var got string
	// Use a TxFactory that captures the context to verify the hook ran.
	capture := uow.TxFactoryFunc(func(ctx context.Context) (uow.Tx, error) {
		v, _ := ctx.Value(ctxKey("key")).(string)
		got = v
		return f.Begin(ctx)
	})

	u2 := uow.New(capture,
		uow.WithBeforeBegin(func(ctx context.Context) context.Context {
			return context.WithValue(ctx, ctxKey("key"), "enriched")
		}),
	)

	err := u2.Do(context.Background(), func(t uow.Tx) error { return nil })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "enriched" {
		t.Fatalf("expected context to be enriched, got %q", got)
	}

	// Also ensure the basic hook doesn't break the flow.
	err = u.Do(context.Background(), func(t uow.Tx) error { return nil })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDo_BeforeCommitHook_Success(t *testing.T) {
	hookRan := false
	tx := &mockTx{}
	f := &mockFactory{tx: tx}
	u := uow.New(f,
		uow.WithBeforeCommit(func(ctx context.Context, t uow.Tx) error {
			hookRan = true
			return nil
		}),
	)

	err := u.Do(context.Background(), func(t uow.Tx) error { return nil })

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hookRan {
		t.Fatal("expected before-commit hook to run")
	}
	if !tx.committed {
		t.Fatal("expected transaction to be committed")
	}
}

func TestDo_BeforeCommitHook_Error(t *testing.T) {
	tx := &mockTx{}
	f := &mockFactory{tx: tx}
	hookErr := errors.New("validation failed")
	u := uow.New(f,
		uow.WithBeforeCommit(func(ctx context.Context, t uow.Tx) error {
			return hookErr
		}),
	)

	err := u.Do(context.Background(), func(t uow.Tx) error { return nil })

	if !errors.Is(err, uow.ErrHookFailed) {
		t.Fatalf("expected ErrHookFailed, got %v", err)
	}
	if !errors.Is(err, hookErr) {
		t.Fatalf("expected underlying hook error, got %v", err)
	}
	if tx.committed {
		t.Fatal("expected transaction NOT to be committed")
	}
	if !tx.rolledBack {
		t.Fatal("expected transaction to be rolled back")
	}
}

func TestDo_AfterRollbackHook(t *testing.T) {
	var hookErr error
	tx := &mockTx{}
	f := &mockFactory{tx: tx}
	bizErr := errors.New("fail")
	u := uow.New(f,
		uow.WithAfterRollback(func(ctx context.Context, err error) {
			hookErr = err
		}),
	)

	err := u.Do(context.Background(), func(t uow.Tx) error { return bizErr })
	if err == nil {
		t.Fatal("expected error")
	}

	if !errors.Is(hookErr, bizErr) {
		t.Fatalf("expected after-rollback hook to receive business error, got %v", hookErr)
	}
}

func TestDo_AfterCommitHook(t *testing.T) {
	hookRan := false
	f := &mockFactory{tx: &mockTx{}}
	u := uow.New(f,
		uow.WithAfterCommit(func(ctx context.Context) {
			hookRan = true
		}),
	)

	err := u.Do(context.Background(), func(t uow.Tx) error { return nil })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hookRan {
		t.Fatal("expected after-commit hook to run")
	}
}

func TestDo_WithLogger(t *testing.T) {
	var buf strings.Builder
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	f := &mockFactory{tx: &mockTx{}}
	u := uow.New(f, uow.WithLogger(logger))

	err := u.Do(context.Background(), func(t uow.Tx) error { return nil })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "beginning transaction") {
		t.Fatalf("expected log to contain 'beginning transaction', got:\n%s", output)
	}
	if !strings.Contains(output, "committed") {
		t.Fatalf("expected log to contain 'committed', got:\n%s", output)
	}
}

func TestTxFactoryFunc(t *testing.T) {
	tx := &mockTx{}
	factory := uow.TxFactoryFunc(func(ctx context.Context) (uow.Tx, error) {
		return tx, nil
	})
	u := uow.New(factory)

	err := u.Do(context.Background(), func(t uow.Tx) error { return nil })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !tx.committed {
		t.Fatal("expected commit")
	}
}

func TestDo_MaxAttemptsClampedToOne(t *testing.T) {
	f := &mockFactory{tx: &mockTx{}}
	u := uow.New(f, uow.WithMaxAttempts(0)) // should clamp to 1

	err := u.Do(context.Background(), func(t uow.Tx) error { return nil })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// Example demonstrates the basic usage pattern.
func Example() {
	// In real code, factory would be backed by a database connection.
	factory := uow.TxFactoryFunc(func(ctx context.Context) (uow.Tx, error) {
		return &noopTx{}, nil
	})

	u := uow.New(factory)

	err := u.Do(context.Background(), func(tx uow.Tx) error {
		// business logic here
		fmt.Println("executed")
		return nil
	})
	if err != nil {
		fmt.Println("error:", err)
	}
	// Output: executed
}

type noopTx struct{}

func (noopTx) Commit() error   { return nil }
func (noopTx) Rollback() error { return nil }
