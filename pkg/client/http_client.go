package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// HTTPClient is a resilient REST client wrapper with multi-role bearer token support
type HTTPClient struct {
	baseURL    string
	httpClient *http.Client
	debug      bool
}

// NewHTTPClient creates a configured HTTP client
func NewHTTPClient(baseURL string, timeout time.Duration) *HTTPClient {
	return &HTTPClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: timeout,
		},
		debug: false,
	}
}

// SetDebug enables/disables verbose HTTP logging
func (c *HTTPClient) SetDebug(debug bool) {
	c.debug = debug
}

// SetBaseURL dynamically changes base URL
func (c *HTTPClient) SetBaseURL(url string) {
	c.baseURL = strings.TrimRight(url, "/")
}

// BaseURL returns current configured base URL
func (c *HTTPClient) BaseURL() string {
	return c.baseURL
}

// Request executes an HTTP request with context, bearer token, body, and unmarshals result
func (c *HTTPClient) Request(ctx context.Context, method, path, token string, body interface{}, responseOut interface{}) (*http.Response, error) {
	return c.RequestWithHeaders(ctx, method, path, token, nil, body, responseOut)
}

// RequestWithHeaders executes an HTTP request with custom headers
func (c *HTTPClient) RequestWithHeaders(ctx context.Context, method, path, token string, headers map[string]string, body interface{}, responseOut interface{}) (*http.Response, error) {
	fullURL := fmt.Sprintf("%s%s", c.baseURL, path)
	var bodyReader io.Reader
	var reqBodyBytes []byte

	if body != nil {
		var err error
		reqBodyBytes, err = json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		bodyReader = bytes.NewBuffer(reqBodyBytes)
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create request [%s %s]: %w", method, fullURL, err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	if c.debug {
		log.Printf("[HTTP >>>] %s %s | Token: %t | Body: %s", method, fullURL, token != "", string(reqBodyBytes))
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request failed [%s %s]: %w", method, fullURL, err)
	}
	defer resp.Body.Close()

	respBodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp, fmt.Errorf("failed to read response body: %w", err)
	}

	if c.debug {
		log.Printf("[HTTP <<<] %s %s -> Status: %d | Body: %s", method, fullURL, resp.StatusCode, string(respBodyBytes))
	}

	// If response is error status (>= 400), return with details
	if resp.StatusCode >= 400 {
		return resp, fmt.Errorf("api error status %d: %s", resp.StatusCode, string(respBodyBytes))
	}

	if responseOut != nil && len(respBodyBytes) > 0 {
		// Handle direct string response (e.g. plain token string or json)
		if strPtr, ok := responseOut.(*string); ok {
			trimmed := strings.Trim(string(respBodyBytes), "\" \r\n")
			*strPtr = trimmed
			return resp, nil
		}

		if err := json.Unmarshal(respBodyBytes, responseOut); err != nil {
			return resp, fmt.Errorf("failed to unmarshal response [%s]: %w", string(respBodyBytes), err)
		}
	}

	return resp, nil
}
