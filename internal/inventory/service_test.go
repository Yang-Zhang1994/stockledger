package inventory_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/Yang-Zhang1994/stockledger/internal/eventstore"
	"github.com/Yang-Zhang1994/stockledger/internal/inventory"
)

func TestConcurrentReservationsNeverOversell(t *testing.T) {
	ctx := context.Background()
	svc := inventory.NewService(eventstore.NewMemory())
	if _, err := svc.Execute(ctx, "SKU-1", func(it *inventory.Item) ([]inventory.Event, error) {
		return it.Receive(10)
	}); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	won := 0
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_, err := svc.Execute(ctx, "SKU-1", func(it *inventory.Item) ([]inventory.Event, error) {
				return it.Reserve(fmt.Sprintf("order-%d", n), 1)
			})
			switch {
			case err == nil:
				mu.Lock()
				won++
				mu.Unlock()
			case errors.Is(err, inventory.ErrInsufficientStock), errors.Is(err, inventory.ErrConflict):
			default:
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()

	it, err := svc.Get(ctx, "SKU-1")
	if err != nil {
		t.Fatal(err)
	}
	if it.Available() < 0 || won != 10-it.Available() {
		t.Fatalf("oversold: won=%d available=%d", won, it.Available())
	}
}

func TestExecuteSkipsAppendWhenCommandIsNoOp(t *testing.T) {
	ctx := context.Background()
	store := eventstore.NewMemory()
	svc := inventory.NewService(store)
	_, _ = svc.Execute(ctx, "SKU-1", func(it *inventory.Item) ([]inventory.Event, error) { return it.Receive(5) })

	it, err := svc.Execute(ctx, "SKU-1", func(it *inventory.Item) ([]inventory.Event, error) { return it.Release("never-reserved") })
	if err != nil || it.Version() != 1 {
		t.Fatalf("version=%v err=%v", it, err)
	}
}
