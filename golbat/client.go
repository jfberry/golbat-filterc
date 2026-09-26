// Package golbat is a minimal client for the Golbat scan API used by
// filterc to run compiled requests.
package golbat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/jfberry/golbat-filterc/filterc"
)

// Client calls a Golbat instance. Secret is sent as X-Golbat-Secret when
// set. A nil HTTP uses http.DefaultClient.
type Client struct {
	URL    string
	Secret string
	HTTP   *http.Client
}

// ScanResponse is Golbat's v3 scan response with the pokemon left as raw
// JSON so nothing is lost or reinterpreted.
type ScanResponse struct {
	Pokemon      []json.RawMessage `json:"pokemon"`
	Examined     int               `json:"examined"`
	Skipped      int               `json:"skipped"`
	Total        int               `json:"total"`
	LimitReached bool              `json:"limit_reached"`
}

// StatusError is a non-2xx reply from Golbat.
type StatusError struct {
	Status int
	Body   string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("golbat returned %d: %s", e.Status, strings.TrimSpace(e.Body))
}

// ScanPokemon posts req to /api/pokemon/v3/scan.
func (c *Client) ScanPokemon(ctx context.Context, req filterc.ScanRequest) (*ScanResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	url := strings.TrimRight(c.URL, "/") + "/api/pokemon/v3/scan"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.Secret != "" {
		httpReq.Header.Set("X-Golbat-Secret", c.Secret)
	}
	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, &StatusError{Status: resp.StatusCode, Body: string(b)}
	}
	var out ScanResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decoding golbat response: %w", err)
	}
	return &out, nil
}
