// Package inventory is the domain model: a StockItem aggregate whose state is
// never stored directly. It is rebuilt by replaying its events, and every
// command either raises new events or returns an error that leaves it unchanged.
package inventory

import (
	"errors"
	"fmt"
)

type EventType string

const (
	StockReceived        EventType = "StockReceived"
	StockReserved        EventType = "StockReserved"
	ReservationReleased  EventType = "ReservationReleased"
	ReservationCommitted EventType = "ReservationCommitted"
)

// Event is a fact that already happened. Version is the aggregate's version
// after the event, so (SKU, Version) is unique and ordered.
type Event struct {
	SKU     string    `json:"sku"`
	Version int       `json:"version"`
	Type    EventType `json:"type"`
	OrderID string    `json:"orderId,omitempty"`
	Qty     int       `json:"qty"`
}

var (
	ErrInvalidQty          = errors.New("quantity must be positive")
	ErrInsufficientStock   = errors.New("insufficient stock")
	ErrReservationMismatch = errors.New("order already holds a different quantity")
	ErrNoReservation       = errors.New("no reservation for order")
	ErrVersionGap          = errors.New("event version out of sequence")
)

// Item is the aggregate root for one SKU.
type Item struct {
	sku      string
	version  int
	onHand   int
	reserved map[string]int // orderID -> qty
}

func New(sku string) *Item {
	return &Item{sku: sku, reserved: map[string]int{}}
}

// Replay rebuilds an Item from its full event history.
func Replay(sku string, history []Event) (*Item, error) {
	it := New(sku)
	for _, e := range history {
		if err := it.Apply(e); err != nil {
			return nil, err
		}
	}
	return it, nil
}

func (it *Item) SKU() string  { return it.sku }
func (it *Item) Version() int { return it.version }
func (it *Item) OnHand() int  { return it.onHand }

func (it *Item) Available() int {
	held := 0
	for _, q := range it.reserved {
		held += q
	}
	return it.onHand - held
}

// Apply mutates state from an event. It is the only place state changes, and it
// refuses events that are not the next version.
func (it *Item) Apply(e Event) error {
	if e.Version != it.version+1 {
		return fmt.Errorf("%w: have v%d, got v%d", ErrVersionGap, it.version, e.Version)
	}
	switch e.Type {
	case StockReceived:
		it.onHand += e.Qty
	case StockReserved:
		it.reserved[e.OrderID] = e.Qty
	case ReservationReleased:
		delete(it.reserved, e.OrderID)
	case ReservationCommitted:
		it.onHand -= it.reserved[e.OrderID]
		delete(it.reserved, e.OrderID)
	default:
		return fmt.Errorf("unknown event type %q", e.Type)
	}
	it.version = e.Version
	return nil
}

func (it *Item) raise(t EventType, orderID string, qty int) ([]Event, error) {
	e := Event{SKU: it.sku, Version: it.version + 1, Type: t, OrderID: orderID, Qty: qty}
	if err := it.Apply(e); err != nil {
		return nil, err
	}
	return []Event{e}, nil
}

func (it *Item) Receive(qty int) ([]Event, error) {
	if qty <= 0 {
		return nil, ErrInvalidQty
	}
	return it.raise(StockReceived, "", qty)
}

// Reserve holds stock for an order. Retrying the same reservation is a no-op,
// so a redelivered request cannot hold stock twice.
func (it *Item) Reserve(orderID string, qty int) ([]Event, error) {
	if qty <= 0 {
		return nil, ErrInvalidQty
	}
	if held, ok := it.reserved[orderID]; ok {
		if held == qty {
			return nil, nil
		}
		return nil, ErrReservationMismatch
	}
	if it.Available() < qty {
		return nil, ErrInsufficientStock
	}
	return it.raise(StockReserved, orderID, qty)
}

// Release returns held stock, e.g. when an unpaid order times out. Releasing
// twice is a no-op.
func (it *Item) Release(orderID string) ([]Event, error) {
	held, ok := it.reserved[orderID]
	if !ok {
		return nil, nil
	}
	return it.raise(ReservationReleased, orderID, held)
}

// Commit turns a reservation into a real deduction once payment succeeds.
func (it *Item) Commit(orderID string) ([]Event, error) {
	held, ok := it.reserved[orderID]
	if !ok {
		return nil, ErrNoReservation
	}
	return it.raise(ReservationCommitted, orderID, held)
}
