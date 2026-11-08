package inventory

import (
	"errors"
	"testing"
)

func received(t *testing.T, qty int) *Item {
	t.Helper()
	it := New("SKU-1")
	if _, err := it.Receive(qty); err != nil {
		t.Fatal(err)
	}
	return it
}

func TestReceiveRaisesVersionedEvent(t *testing.T) {
	it := New("SKU-1")
	evs, err := it.Receive(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 || evs[0].Type != StockReceived || evs[0].Version != 1 || evs[0].Qty != 10 {
		t.Fatalf("unexpected events: %+v", evs)
	}
	if it.OnHand() != 10 || it.Available() != 10 || it.Version() != 1 {
		t.Fatalf("state: onHand=%d available=%d version=%d", it.OnHand(), it.Available(), it.Version())
	}
}

func TestReceiveRejectsNonPositiveQty(t *testing.T) {
	if _, err := New("SKU-1").Receive(0); !errors.Is(err, ErrInvalidQty) {
		t.Fatalf("want ErrInvalidQty, got %v", err)
	}
}

func TestReserveHoldsStockWithoutReducingOnHand(t *testing.T) {
	it := received(t, 10)
	if _, err := it.Reserve("order-1", 3); err != nil {
		t.Fatal(err)
	}
	if it.OnHand() != 10 || it.Available() != 7 {
		t.Fatalf("onHand=%d available=%d", it.OnHand(), it.Available())
	}
}

func TestReserveNeverOversells(t *testing.T) {
	it := received(t, 5)
	if _, err := it.Reserve("order-1", 4); err != nil {
		t.Fatal(err)
	}
	if _, err := it.Reserve("order-2", 2); !errors.Is(err, ErrInsufficientStock) {
		t.Fatalf("want ErrInsufficientStock, got %v", err)
	}
	if it.Available() != 1 {
		t.Fatalf("available=%d", it.Available())
	}
}

func TestReserveSameOrderTwiceIsIdempotent(t *testing.T) {
	it := received(t, 5)
	if _, err := it.Reserve("order-1", 2); err != nil {
		t.Fatal(err)
	}
	evs, err := it.Reserve("order-1", 2)
	if err != nil || len(evs) != 0 {
		t.Fatalf("retry should be a no-op, got events=%v err=%v", evs, err)
	}
	if it.Available() != 3 {
		t.Fatalf("available=%d", it.Available())
	}
}

func TestReserveSameOrderDifferentQtyIsRejected(t *testing.T) {
	it := received(t, 5)
	if _, err := it.Reserve("order-1", 2); err != nil {
		t.Fatal(err)
	}
	if _, err := it.Reserve("order-1", 3); !errors.Is(err, ErrReservationMismatch) {
		t.Fatalf("want ErrReservationMismatch, got %v", err)
	}
}

func TestReleaseReturnsStockAndIsIdempotent(t *testing.T) {
	it := received(t, 5)
	if _, err := it.Reserve("order-1", 2); err != nil {
		t.Fatal(err)
	}
	if _, err := it.Release("order-1"); err != nil {
		t.Fatal(err)
	}
	evs, err := it.Release("order-1")
	if err != nil || len(evs) != 0 {
		t.Fatalf("second release should be a no-op, got events=%v err=%v", evs, err)
	}
	if it.Available() != 5 {
		t.Fatalf("available=%d", it.Available())
	}
}

func TestCommitDeductsOnHand(t *testing.T) {
	it := received(t, 5)
	if _, err := it.Reserve("order-1", 2); err != nil {
		t.Fatal(err)
	}
	if _, err := it.Commit("order-1"); err != nil {
		t.Fatal(err)
	}
	if it.OnHand() != 3 || it.Available() != 3 {
		t.Fatalf("onHand=%d available=%d", it.OnHand(), it.Available())
	}
}

func TestCommitWithoutReservationFails(t *testing.T) {
	it := received(t, 5)
	if _, err := it.Commit("missing"); !errors.Is(err, ErrNoReservation) {
		t.Fatalf("want ErrNoReservation, got %v", err)
	}
}

func TestReplayRebuildsSameState(t *testing.T) {
	it := received(t, 10)
	var history []Event
	history = append(history, Event{SKU: "SKU-1", Version: 1, Type: StockReceived, Qty: 10})
	for _, step := range []func() ([]Event, error){
		func() ([]Event, error) { return it.Reserve("a", 4) },
		func() ([]Event, error) { return it.Reserve("b", 1) },
		func() ([]Event, error) { return it.Commit("a") },
		func() ([]Event, error) { return it.Release("b") },
	} {
		evs, err := step()
		if err != nil {
			t.Fatal(err)
		}
		history = append(history, evs...)
	}

	replayed, err := Replay("SKU-1", history)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.OnHand() != it.OnHand() || replayed.Available() != it.Available() || replayed.Version() != it.Version() {
		t.Fatalf("replayed=%d/%d/v%d live=%d/%d/v%d",
			replayed.OnHand(), replayed.Available(), replayed.Version(), it.OnHand(), it.Available(), it.Version())
	}
}

func TestReplayRejectsVersionGap(t *testing.T) {
	_, err := Replay("SKU-1", []Event{
		{SKU: "SKU-1", Version: 1, Type: StockReceived, Qty: 5},
		{SKU: "SKU-1", Version: 3, Type: StockReceived, Qty: 5},
	})
	if !errors.Is(err, ErrVersionGap) {
		t.Fatalf("want ErrVersionGap, got %v", err)
	}
}
