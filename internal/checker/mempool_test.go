package checker

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestMempoolCheckAddressesReturnsConfirmedBalance(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/address/") {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.Error(w, "bad path", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"address":"bc1xyz","chain_stats":{"funded_txo_sum":10000,"spent_txo_sum":2500},"mempool_stats":{"funded_txo_sum":0,"spent_txo_sum":0}}`)
	}))
	defer srv.Close()

	c := newMempoolClient(srv.URL+"/api", srv.Client())
	got, err := c.CheckAddresses(context.Background(), []string{"bc1xyz"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 7500 {
		t.Errorf("got %d, want 7500", got)
	}
}

func TestMempoolCheckAddressesIgnoresMempoolBalance(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"address":"bc1xyz","chain_stats":{"funded_txo_sum":10000,"spent_txo_sum":0},"mempool_stats":{"funded_txo_sum":99999,"spent_txo_sum":0}}`)
	}))
	defer srv.Close()

	c := newMempoolClient(srv.URL+"/api", srv.Client())
	got, err := c.CheckAddresses(context.Background(), []string{"bc1xyz"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 10000 {
		t.Errorf("got %d, want 10000 (mempool balance must be ignored)", got)
	}
}

func TestMempoolCheckAddressesSumsAcrossMultipleAddresses(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/addr1"):
			_, _ = fmt.Fprint(w, `{"address":"addr1","chain_stats":{"funded_txo_sum":1000,"spent_txo_sum":250},"mempool_stats":{"funded_txo_sum":0,"spent_txo_sum":0}}`)
		case strings.HasSuffix(r.URL.Path, "/addr2"):
			_, _ = fmt.Fprint(w, `{"address":"addr2","chain_stats":{"funded_txo_sum":4000,"spent_txo_sum":1000},"mempool_stats":{"funded_txo_sum":0,"spent_txo_sum":0}}`)
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.Error(w, "bad path", http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := newMempoolClient(srv.URL+"/api", srv.Client())
	got, err := c.CheckAddresses(context.Background(), []string{"addr1", "addr2"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// (1000 - 250) + (4000 - 1000) = 750 + 3000 = 3750
	if got != 3750 {
		t.Errorf("got %d, want 3750", got)
	}
}

func TestMempoolCheckAddresses429ReturnsErrRateLimited(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := newMempoolClient(srv.URL+"/api", srv.Client())
	_, err := c.CheckAddresses(context.Background(), []string{"bc1xyz"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrRateLimited) {
		t.Errorf("expected ErrRateLimited, got %v", err)
	}
}

func TestMempoolCheckAddresses500ReturnsErrUnexpected(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := newMempoolClient(srv.URL+"/api", srv.Client())
	_, err := c.CheckAddresses(context.Background(), []string{"bc1xyz"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrUnexpected) {
		t.Errorf("expected ErrUnexpected, got %v", err)
	}
}

func TestMempoolCheckAddressesEmptyListReturnsZero(t *testing.T) {
	t.Parallel()

	var called atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called.Store(true)
		_, _ = fmt.Fprint(w, `{}`)
	}))
	defer srv.Close()

	c := newMempoolClient(srv.URL+"/api", srv.Client())
	got, err := c.CheckAddresses(context.Background(), []string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 0 {
		t.Errorf("got %d, want 0", got)
	}
	if called.Load() {
		t.Error("HTTP server should not have been called for empty address slice")
	}
}

func TestNewMempoolProvider(t *testing.T) {
	t.Parallel()

	c, err := New(ProviderMempool, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c == nil {
		t.Fatal("expected non-nil checker")
	}
}
