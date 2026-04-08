// Package checker queries public block-explorer APIs for the confirmed
// on-chain balance of one or more Bitcoin addresses. It is intentionally
// minimal: implementations only need to expose CheckAddresses, and a
// rate-limit wrapper is provided so the pipeline can stay polite to public
// services like mempool.space and blockstream.info.
package checker

import (
	"context"
	"errors"
	"net/http"
	"time"
)

// Provider names a built-in balance checker.
type Provider string

const (
	// ProviderMempool selects the mempool.space Esplora API.
	ProviderMempool Provider = "mempool"
	// ProviderBlockstream selects the blockstream.info Esplora API.
	ProviderBlockstream Provider = "blockstream"
)

// Public base URLs for the supported providers.
const (
	mempoolBaseURL     = "https://mempool.space/api"
	blockstreamBaseURL = "https://blockstream.info/api"
)

// defaultHTTPTimeout is applied when the caller passes a nil *http.Client to
// New so unbounded hangs can never sneak into the pipeline.
const defaultHTTPTimeout = 15 * time.Second

// AddressBalance is the result of a single address lookup.
type AddressBalance struct {
	Address     string
	BalanceSats int64
}

// BalanceChecker checks the on-chain balance of one or more addresses.
// Implementations must be safe for concurrent use.
type BalanceChecker interface {
	// CheckAddresses returns the total confirmed balance in satoshis across
	// all given addresses. The slice may have one or more entries.
	// Implementations should return a single error if any single address
	// lookup failed (use errors.Join when bundling).
	CheckAddresses(ctx context.Context, addresses []string) (int64, error)
}

// Sentinel errors. Callers should branch with errors.Is.
var (
	// ErrRateLimited indicates the upstream API rejected the request with
	// HTTP 429.
	ErrRateLimited = errors.New("checker: rate limited by upstream")
	// ErrUnexpected indicates an upstream response we could not interpret
	// (non-2xx status, malformed JSON, transport failure, ...).
	ErrUnexpected = errors.New("checker: unexpected response")
	// ErrUnknownProvider is returned by New when given a provider name it
	// does not recognise.
	ErrUnknownProvider = errors.New("checker: unknown provider")
)

// New constructs a BalanceChecker for the given provider. If httpClient is
// nil a default *http.Client with a sensible timeout is used. Returns
// ErrUnknownProvider for an unrecognised provider name.
func New(provider Provider, httpClient *http.Client) (BalanceChecker, error) {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultHTTPTimeout}
	}
	switch provider {
	case ProviderMempool:
		return newMempoolClient(mempoolBaseURL, httpClient), nil
	case ProviderBlockstream:
		return newBlockstreamClient(blockstreamBaseURL, httpClient), nil
	default:
		return nil, ErrUnknownProvider
	}
}
