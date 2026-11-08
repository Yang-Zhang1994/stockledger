// Package api exposes commands (writes) and queries (reads) over HTTP.
//
//	POST   /skus/{sku}/receive                       {"qty": 10}
//	POST   /skus/{sku}/reservations                  {"orderId": "o1", "qty": 2}
//	DELETE /skus/{sku}/reservations/{orderId}
//	POST   /skus/{sku}/reservations/{orderId}/commit
//	GET    /skus/{sku}            state rebuilt from the event store
//	GET    /skus/{sku}/events     the raw event history
//	GET    /available/{sku}       read model fed by Kafka (eventually consistent)
package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/Yang-Zhang1994/stockledger/internal/inventory"
	"github.com/Yang-Zhang1994/stockledger/internal/projection"
)

type Handler struct {
	svc    *inventory.Service
	stream inventory.Stream
	read   *projection.Available
}

func New(svc *inventory.Service, stream inventory.Stream, read *projection.Available) http.Handler {
	h := &Handler{svc: svc, stream: stream, read: read}
	mux := http.NewServeMux()
	mux.HandleFunc("/skus/", h.skus)
	mux.HandleFunc("/available/", h.available)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	return mux
}

type itemView struct {
	SKU       string `json:"sku"`
	OnHand    int    `json:"onHand"`
	Available int    `json:"available"`
	Version   int    `json:"version"`
}

func view(it *inventory.Item) itemView {
	return itemView{it.SKU(), it.OnHand(), it.Available(), it.Version()}
}

func (h *Handler) skus(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/skus/"), "/"), "/")
	sku := parts[0]
	if sku == "" {
		http.NotFound(w, r)
		return
	}
	route := r.Method + " " + strings.Join(parts[1:], "/")
	ctx := r.Context()

	var cmd func(*inventory.Item) ([]inventory.Event, error)
	switch {
	case route == "GET ":
		it, err := h.svc.Get(ctx, sku)
		if err != nil {
			reply(w, 0, nil, err)
			return
		}
		reply(w, http.StatusOK, view(it), nil)
		return
	case route == "GET events":
		evs, err := h.stream.Load(ctx, sku)
		if evs == nil {
			evs = []inventory.Event{}
		}
		reply(w, http.StatusOK, evs, err)
		return
	case route == "POST receive":
		var body struct{ Qty int }
		if !decode(w, r, &body) {
			return
		}
		cmd = func(it *inventory.Item) ([]inventory.Event, error) { return it.Receive(body.Qty) }
	case route == "POST reservations":
		var body struct {
			OrderID string
			Qty     int
		}
		if !decode(w, r, &body) {
			return
		}
		if body.OrderID == "" {
			reply(w, 0, nil, errBadRequest("orderId is required"))
			return
		}
		cmd = func(it *inventory.Item) ([]inventory.Event, error) { return it.Reserve(body.OrderID, body.Qty) }
	case r.Method == http.MethodDelete && len(parts) == 3 && parts[1] == "reservations":
		cmd = func(it *inventory.Item) ([]inventory.Event, error) { return it.Release(parts[2]) }
	case r.Method == http.MethodPost && len(parts) == 4 && parts[1] == "reservations" && parts[3] == "commit":
		cmd = func(it *inventory.Item) ([]inventory.Event, error) { return it.Commit(parts[2]) }
	default:
		http.NotFound(w, r)
		return
	}
	it, err := h.svc.Execute(ctx, sku, cmd)
	if err != nil {
		reply(w, 0, nil, err)
		return
	}
	reply(w, http.StatusOK, view(it), nil)
}

func (h *Handler) available(w http.ResponseWriter, r *http.Request) {
	sku := strings.Trim(strings.TrimPrefix(r.URL.Path, "/available/"), "/")
	avail, version, ok := h.read.Get(sku)
	if !ok {
		http.NotFound(w, r)
		return
	}
	reply(w, http.StatusOK, map[string]any{"sku": sku, "available": avail, "version": version}, nil)
}

type errBadRequest string

func (e errBadRequest) Error() string { return string(e) }

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		reply(w, 0, nil, errBadRequest("invalid JSON body"))
		return false
	}
	return true
}

func status(err error) int {
	var bad errBadRequest
	switch {
	case errors.As(err, &bad), errors.Is(err, inventory.ErrInvalidQty):
		return http.StatusBadRequest
	case errors.Is(err, inventory.ErrNoReservation):
		return http.StatusNotFound
	case errors.Is(err, inventory.ErrInsufficientStock),
		errors.Is(err, inventory.ErrReservationMismatch),
		errors.Is(err, inventory.ErrConflict):
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}

func reply(w http.ResponseWriter, code int, body any, err error) {
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		code = status(err)
		msg := err.Error()
		if code == http.StatusInternalServerError {
			msg = "internal error"
		}
		body = map[string]string{"error": msg}
	}
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}
