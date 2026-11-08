package inventory

import (
	"context"
	"errors"
)

// Stream is the slice of the event store the service needs. Defining it here
// keeps the domain free of storage details.
type Stream interface {
	Load(ctx context.Context, sku string) ([]Event, error)
	Append(ctx context.Context, sku string, expectedVersion int, events []Event) error
}

// ErrConflict must be returned by Stream.Append when the stream moved on.
// The event store wraps its own conflict error with this one.
var ErrConflict = errors.New("concurrent update")

const maxAttempts = 5

// Service runs one command against one SKU: load history, rebuild the
// aggregate, decide, then append. On a concurrent write it reloads and retries.
type Service struct{ stream Stream }

func NewService(s Stream) *Service { return &Service{stream: s} }

func (s *Service) Get(ctx context.Context, sku string) (*Item, error) {
	history, err := s.stream.Load(ctx, sku)
	if err != nil {
		return nil, err
	}
	return Replay(sku, history)
}

func (s *Service) Execute(ctx context.Context, sku string, cmd func(*Item) ([]Event, error)) (*Item, error) {
	var err error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		var it *Item
		if it, err = s.Get(ctx, sku); err != nil {
			return nil, err
		}
		before := it.Version()
		var events []Event
		if events, err = cmd(it); err != nil {
			return nil, err
		}
		if len(events) == 0 {
			return it, nil
		}
		if err = s.stream.Append(ctx, sku, before, events); errors.Is(err, ErrConflict) {
			continue
		}
		return it, err
	}
	return nil, err
}
