package quotes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	httpClient *http.Client
	quoteURL   string
}

type response struct {
	Quote  string `json:"q"`
	Author string `json:"a"`
}

func NewClient(quoteURL string, timeout time.Duration) *Client {
	return &Client{
		httpClient: &http.Client{Timeout: timeout},
		quoteURL:   quoteURL,
	}
}

func (qc *Client) GetRandomQuote(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, qc.quoteURL, nil)
	if err != nil {
		return "", err
	}

	resp, err := qc.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("zenquotes: unexpected status code: %d", resp.StatusCode)
	}

	var quotes []response
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&quotes); err != nil {
		return "", err
	}

	if len(quotes) == 0 || strings.TrimSpace(quotes[0].Quote) == "" {
		return "", errors.New("zenquotes: empty response")
	}

	return strings.Join(strings.Fields(quotes[0].Quote), " "), nil
}
