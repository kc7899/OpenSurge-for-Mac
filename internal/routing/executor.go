package routing

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Selector is deliberately limited: lifecycle/configuration actions cannot be
// reached through this interface. Revision must include settings and graph.
type Selector interface {
	Revision(context.Context) (string, error)
	Current(context.Context) (string, error)
	Select(context.Context, string) error
}

const selectorVerifyTimeout = 3 * time.Second

var ErrStale = errors.New("routing snapshot changed")
var ErrBusy = errors.New("routing selection is busy")

type Executor struct{ mu sync.Mutex }

func (e *Executor) Apply(ctx context.Context, api Selector, version, expected string, d Decision) (bool, error) {
	if !e.mu.TryLock() {
		return false, ErrBusy
	}
	defer e.mu.Unlock()
	if !d.Change {
		return false, nil
	}
	if d.Target == "" {
		return false, fmt.Errorf("empty selection target")
	}
	rev, err := api.Revision(ctx)
	if err != nil {
		return false, err
	}
	if rev != version {
		return false, ErrStale
	}
	current, err := api.Current(ctx)
	if err != nil {
		return false, err
	}
	if current != expected {
		return false, ErrStale
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	selectErr := api.Select(ctx, d.Target)
	// A timeout may follow a successful remote commit. Read back with a separate
	// bounded context supplied by the adapter's own HTTP timeout; never retry PUT.
	verifyCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), selectorVerifyTimeout)
	defer cancel()
	selected, err := api.Current(verifyCtx)
	if err != nil {
		return false, fmt.Errorf("selection outcome unknown: %w", err)
	}
	if selected == d.Target {
		return true, nil
	}
	if selectErr != nil {
		return false, selectErr
	}
	return false, fmt.Errorf("selection not confirmed")
}
