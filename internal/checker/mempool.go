package checker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// esploraStats mirrors the chain_stats / mempool_stats objects returned by
// any Esplora-compatible API.
type esploraStats struct {
	FundedTxoSum int64 `json:"funded_txo_sum"`
	SpentTxoSum  int64 `json:"spent_txo_sum"`
}

// esploraResponse is the subset of the /address/{addr} response we care
// about. The mempool_stats field is intentionally omitted: we only count
// confirmed balances.
type esploraResponse struct {
	ChainStats esploraStats `json:"chain_stats"`
}

// mempoolClient queries the mempool.space Esplora API.
type mempoolClient struct {
	baseURL string
	http    *http.Client
}

func newMempoolClient(baseURL string, h *http.Client) *mempoolClient {
	return &mempoolClient{baseURL: baseURL, http: h}
}

// CheckAddresses returns the total confirmed satoshi balance across the
// supplied addresses. An empty slice short-circuits to (0, nil) without
// touching the network.
func (c *mempoolClient) CheckAddresses(ctx context.Context, addresses []string) (int64, error) {
	return fetchEsploraTotal(ctx, c.http, c.baseURL, addresses)
}

// fetchEsploraTotal is shared by every Esplora-compatible client. It is the
// single place that knows how to talk to /api/address/{addr}, parse the
// response, classify HTTP errors, and bundle per-address failures.
func fetchEsploraTotal(ctx context.Context, httpClient *http.Client, baseURL string, addresses []string) (int64, error) {
	if len(addresses) == 0 {
		return 0, nil
	}

	var (
		total int64
		errs  []error
	)
	for _, addr := range addresses {
		balance, err := fetchEsploraAddress(ctx, httpClient, baseURL, addr)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		total += balance
	}
	if len(errs) > 0 {
		return 0, errors.Join(errs...)
	}
	return total, nil
}

func fetchEsploraAddress(ctx context.Context, httpClient *http.Client, baseURL, address string) (int64, error) {
	url := fmt.Sprintf("%s/address/%s", baseURL, address)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, fmt.Errorf("%w: build request: %v", ErrUnexpected, err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("%w: http do: %v", ErrUnexpected, err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		return 0, fmt.Errorf("%w: status %d for %s", ErrRateLimited, resp.StatusCode, address)
	case resp.StatusCode < 200 || resp.StatusCode >= 300:
		return 0, fmt.Errorf("%w: status %d for %s", ErrUnexpected, resp.StatusCode, address)
	}

	var parsed esploraResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return 0, fmt.Errorf("%w: decode body: %v", ErrUnexpected, err)
	}

	return parsed.ChainStats.FundedTxoSum - parsed.ChainStats.SpentTxoSum, nil
}
