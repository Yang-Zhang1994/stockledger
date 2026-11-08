// Package projection builds the query side (CQRS read model) from events
// received over Kafka. It is eventually consistent with the event store.
package projection

import (
	"sync"

	"github.com/Yang-Zhang1994/stockledger/internal/inventory"
)

type Available struct {
	mu    sync.RWMutex
	items map[string]*inventory.Item
}

func NewAvailable() *Available {
	return &Available{items: map[string]*inventory.Item{}}
}

// Handle applies one event. Kafka delivers at-least-once, so an event at or
// below the version already applied is a duplicate and is skipped.
func (p *Available) Handle(e inventory.Event) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	it, ok := p.items[e.SKU]
	if !ok {
		it = inventory.New(e.SKU)
		p.items[e.SKU] = it
	}
	if e.Version <= it.Version() {
		return nil
	}
	return it.Apply(e)
}

func (p *Available) Get(sku string) (available, version int, ok bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	it, ok := p.items[sku]
	if !ok {
		return 0, 0, false
	}
	return it.Available(), it.Version(), true
}
