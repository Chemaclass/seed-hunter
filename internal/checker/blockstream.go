package checker

import (
	"context"
	"net/http"
)

// blockstreamClient queries the blockstream.info Esplora API. Esplora is the
// reference implementation that mempool.space also speaks, so the request
// shape and response parsing live in fetchEsploraTotal — this client is
// just a thin wrapper that pins the base URL.
type blockstreamClient struct {
	baseURL string
	http    *http.Client
}

func newBlockstreamClient(baseURL string, h *http.Client) *blockstreamClient {
	return &blockstreamClient{baseURL: baseURL, http: h}
}

// CheckAddresses returns the total confirmed satoshi balance across the
// supplied addresses. An empty slice short-circuits to (0, nil) without
// touching the network.
func (c *blockstreamClient) CheckAddresses(ctx context.Context, addresses []string) (int64, error) {
	return fetchEsploraTotal(ctx, c.http, c.baseURL, addresses)
}
