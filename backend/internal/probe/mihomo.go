package probe

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type DelayClient interface {
	Delay(ctx context.Context, proxyName string, delayURL string, timeoutMS int) (float64, error)
}

type MihomoClient struct {
	baseURL string
	secret  string
	client  *http.Client
}

func NewMihomoClient(baseURL string, secret string, timeoutMS int) *MihomoClient {
	timeout := time.Duration(timeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &MihomoClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		secret:  secret,
		client:  &http.Client{Timeout: timeout},
	}
}

func (c *MihomoClient) Delay(ctx context.Context, proxyName string, delayURL string, timeoutMS int) (float64, error) {
	endpoint := fmt.Sprintf("%s/proxies/%s/delay", c.baseURL, url.PathEscape(proxyName))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, err
	}
	if c.secret != "" {
		req.Header.Set("Authorization", "Bearer "+c.secret)
	}
	query := req.URL.Query()
	query.Set("url", delayURL)
	query.Set("timeout", strconv.Itoa(timeoutMS))
	req.URL.RawQuery = query.Encode()

	response, err := c.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return 0, fmt.Errorf("mihomo delay failed with status %d", response.StatusCode)
	}
	var payload struct {
		Delay *float64 `json:"delay"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return 0, err
	}
	if payload.Delay == nil {
		return 0, fmt.Errorf("mihomo delay response missing delay")
	}
	return *payload.Delay, nil
}
