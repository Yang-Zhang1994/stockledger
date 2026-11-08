package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Yang-Zhang1994/stockledger/internal/eventstore"
	"github.com/Yang-Zhang1994/stockledger/internal/inventory"
	"github.com/Yang-Zhang1994/stockledger/internal/projection"
	"github.com/Yang-Zhang1994/stockledger/internal/relay"
)

type env struct {
	srv   *httptest.Server
	relay *relay.Relay
}

func newEnv(t *testing.T) env {
	store := eventstore.NewMemory()
	read := projection.NewAvailable()
	srv := httptest.NewServer(New(inventory.NewService(store), store, read))
	t.Cleanup(srv.Close)
	return env{srv, relay.New(store, relay.LocalPublisher(read.Handle))}
}

func (e env) do(t *testing.T, method, path, body string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(method, e.srv.URL+path, strings.NewReader(body))
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(res.Body).Decode(&out)
	return res.StatusCode, out
}

func TestCheckoutFlowOverHTTP(t *testing.T) {
	e := newEnv(t)
	if code, _ := e.do(t, "POST", "/skus/SKU-1/receive", `{"qty":5}`); code != 200 {
		t.Fatalf("receive: %d", code)
	}
	if code, body := e.do(t, "POST", "/skus/SKU-1/reservations", `{"orderId":"o1","qty":3}`); code != 200 || body["available"] != 2.0 {
		t.Fatalf("reserve: %d %v", code, body)
	}
	if code, body := e.do(t, "POST", "/skus/SKU-1/reservations", `{"orderId":"o2","qty":3}`); code != 409 {
		t.Fatalf("oversell should be 409, got %d %v", code, body)
	}
	if code, body := e.do(t, "POST", "/skus/SKU-1/reservations/o1/commit", ``); code != 200 || body["onHand"] != 2.0 {
		t.Fatalf("commit: %d %v", code, body)
	}
}

func TestReadModelCatchesUpAfterRelay(t *testing.T) {
	e := newEnv(t)
	e.do(t, "POST", "/skus/SKU-1/receive", `{"qty":5}`)
	e.do(t, "POST", "/skus/SKU-1/reservations", `{"orderId":"o1","qty":2}`)

	if code, _ := e.do(t, "GET", "/available/SKU-1", ""); code != 404 {
		t.Fatalf("read model should be empty before relay, got %d", code)
	}
	if _, err := e.relay.Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	if code, body := e.do(t, "GET", "/available/SKU-1", ""); code != 200 || body["available"] != 3.0 {
		t.Fatalf("after relay: %d %v", code, body)
	}
}

func TestBadInputReturns400(t *testing.T) {
	e := newEnv(t)
	for _, c := range []struct{ path, body string }{
		{"/skus/SKU-1/receive", `{"qty":0}`},
		{"/skus/SKU-1/receive", `not json`},
		{"/skus/SKU-1/reservations", `{"qty":1}`},
	} {
		if code, _ := e.do(t, "POST", c.path, c.body); code != 400 {
			t.Errorf("%s %s: want 400, got %d", c.path, c.body, code)
		}
	}
}
