package eventstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/Yang-Zhang1994/stockledger/internal/inventory"
)

// Each SKU's events form a hash chain: an event's hash covers its own fields
// and the hash of the event before it. Editing, deleting or reordering any
// stored event changes a hash that a later event depends on, so Verify finds
// it. The chain is per SKU, like the optimistic lock, so writers to different
// SKUs never wait on each other.
//
// What a chain alone cannot catch: dropping the newest events, or rewriting an
// event and recomputing every hash after it. Both change the head hash, so
// recording the head somewhere the database cannot touch closes that gap.

// Sealed is a stored event together with its chain hash.
type Sealed struct {
	inventory.Event
	Hash string `json:"hash"`
}

// Auditor reads a SKU's history with hashes, for verification.
type Auditor interface {
	History(ctx context.Context, sku string) ([]Sealed, error)
}

// Hash returns the chain hash of e given the previous event's hash ("" for
// the first event). Fields are encoded as a JSON array so no two different
// events can produce the same input bytes.
func Hash(prev string, e inventory.Event) string {
	b, _ := json.Marshal([]any{prev, e.SKU, e.Version, e.Type, e.OrderID, e.Qty})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Report is the result of verifying one SKU's chain.
type Report struct {
	SKU    string `json:"sku"`
	Events int    `json:"events"`
	OK     bool   `json:"ok"`
	// Head is the hash of the last event; record it elsewhere to detect truncation.
	Head string `json:"head,omitempty"`
	// BrokenAt is the version of the first event that fails, when OK is false.
	BrokenAt int    `json:"brokenAt,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// Verify recomputes the chain from the first event and stops at the first
// event that is missing, out of order, or does not match its stored hash.
func Verify(sku string, history []Sealed) Report {
	r := Report{SKU: sku, Events: len(history), OK: true}
	prev := ""
	for i, e := range history {
		want := i + 1
		switch {
		case e.SKU != sku:
			return r.fail(want, fmt.Sprintf("event belongs to SKU %q", e.SKU))
		case e.Version != want:
			return r.fail(want, fmt.Sprintf("expected version %d, found %d (missing or reordered event)", want, e.Version))
		case e.Hash != Hash(prev, e.Event):
			return r.fail(want, "hash mismatch (event edited, or an earlier event changed)")
		}
		prev = e.Hash
	}
	r.Head = prev
	return r
}

func (r Report) fail(version int, reason string) Report {
	r.OK, r.BrokenAt, r.Reason = false, version, reason
	return r
}

// seal chains events onto prev, returning them with their hashes.
func seal(prev string, events []inventory.Event) []Sealed {
	out := make([]Sealed, len(events))
	for i, e := range events {
		prev = Hash(prev, e)
		out[i] = Sealed{e, prev}
	}
	return out
}
