package profile

import (
	"context"
	"fmt"
	"io"
	"net/http"
)

// Fetcher retrieves the raw bytes of a pprof profile from a Source.
// The returned slice is in the binary protobuf format.
type Fetcher interface {
	Fetch(ctx context.Context, src Source) ([]byte, error)
}

// HTTPFetcher fetches profiles over HTTP from a net/http/pprof
// endpoint. A nil Client falls back to http.DefaultClient.
type HTTPFetcher struct {
	Client *http.Client
}

// NewHTTPFetcher returns an HTTPFetcher with a client that imposes
// no overall, response-header, or idle timeouts of its own. The Go
// runtime's /debug/pprof/profile handler does not flush response
// headers until the sample window is nearly over.
func NewHTTPFetcher() *HTTPFetcher {
	return &HTTPFetcher{Client: &http.Client{}}
}

// maxProfileBytes caps how much of a response Fetch reads. Real
// pprof profiles run from kilobytes to a few megabytes; the cap only
// exists so a misbehaving or hostile endpoint cannot exhaust memory.
const maxProfileBytes = 128 << 20 // 128 MiB

// Fetch implements Fetcher.
func (f *HTTPFetcher) Fetch(ctx context.Context, src Source) (raw []byte, err error) {
	u, err := src.URL()
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	client := f.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", u, err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("close response body: %w", cerr)
		}
	}()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch %s: status %d", u, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxProfileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	if len(body) > maxProfileBytes {
		return nil, fmt.Errorf("fetch %s: response exceeds %d-byte profile cap", u, maxProfileBytes)
	}
	if len(body) == 0 {
		return nil, fmt.Errorf("fetch %s: empty body", u)
	}
	return body, nil
}
