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

func TestBlockstreamCheckAddressesReturnsConfirmedBalance(t *testing.T) {
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

	c := newBlockstreamClient(srv.URL+"/api", srv.Client())
	got, err := c.CheckAddresses(context.Background(), []string{"bc1xyz"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 7500 {
		t.Errorf("got %d, want 7500", got)
	}
}

func TestBlockstreamCheckAddressesIgnoresMempoolBalance(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"address":"bc1xyz","chain_stats":{"funded_txo_sum":10000,"spent_txo_sum":0},"mempool_stats":{"funded_txo_sum":99999,"spent_txo_sum":0}}`)
	}))
	defer srv.Close()

	c := newBlockstreamClient(srv.URL+"/api", srv.Client())
	got, err := c.CheckAddresses(context.Background(), []string{"bc1xyz"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 10000 {
		t.Errorf("got %d, want 10000 (mempool balance must be ignored)", got)
	}
}

func TestBlockstreamCheckAddressesSumsAcrossMultipleAddresses(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/addr1"):
			_, _ = fmt.Fprint(w, `{"address":"addr1","chain_stats":{"funded_txo_sum":2000,"spent_txo_sum":500},"mempool_stats":{"funded_txo_sum":0,"spent_txo_sum":0}}`)
		case strings.HasSuffix(r.URL.Path, "/addr2"):
			_, _ = fmt.Fprint(w, `{"address":"addr2","chain_stats":{"funded_txo_sum":8000,"spent_txo_sum":3000},"mempool_stats":{"funded_txo_sum":0,"spent_txo_sum":0}}`)
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.Error(w, "bad path", http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := newBlockstreamClient(srv.URL+"/api", srv.Client())
	got, err := c.CheckAddresses(context.Background(), []string{"addr1", "addr2"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// (2000 - 500) + (8000 - 3000) = 1500 + 5000 = 6500
	if got != 6500 {
		t.Errorf("got %d, want 6500", got)
	}
}

func TestBlockstreamCheckAddresses429ReturnsErrRateLimited(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := newBlockstreamClient(srv.URL+"/api", srv.Client())
	_, err := c.CheckAddresses(context.Background(), []string{"bc1xyz"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrRateLimited) {
		t.Errorf("expected ErrRateLimited, got %v", err)
	}
}

func TestBlockstreamCheckAddresses500ReturnsErrUnexpected(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := newBlockstreamClient(srv.URL+"/api", srv.Client())
	_, err := c.CheckAddresses(context.Background(), []string{"bc1xyz"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrUnexpected) {
		t.Errorf("expected ErrUnexpected, got %v", err)
	}
}

func TestBlockstreamCheckAddressesEmptyListReturnsZero(t *testing.T) {
	t.Parallel()

	var called atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called.Store(true)
		_, _ = fmt.Fprint(w, `{}`)
	}))
	defer srv.Close()

	c := newBlockstreamClient(srv.URL+"/api", srv.Client())
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

func TestNewBlockstreamProvider(t *testing.T) {
	t.Parallel()

	c, err := New(ProviderBlockstream, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c == nil {
		t.Fatal("expected non-nil checker")
	}
}

func TestNewUnknownProvider(t *testing.T) {
	t.Parallel()

	_, err := New(Provider("nope"), nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrUnknownProvider) {
		t.Errorf("expected ErrUnknownProvider, got %v", err)
	}
}
