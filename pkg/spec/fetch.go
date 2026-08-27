package spec

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// fetchClient downloads specifications: 30s overall timeout, TLS
// certificate verification disabled (self-signed internal stands).
var fetchClient = &http.Client{
	Timeout: 30 * time.Second,
	Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // internal stands use self-signed certs
	},
}

// Fetch downloads a specification document from url, capping the body at
// maxBodyBytes.
func Fetch(ctx context.Context, rawURL string) ([]byte, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, fmt.Errorf("url обязателен")
	}
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		return nil, fmt.Errorf("url должен начинаться с http:// или https://, получено %q", rawURL)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("некорректный url %q: %w", rawURL, err)
	}
	resp, err := fetchClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("не удалось скачать спецификацию: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("при скачивании спецификации получен статус %d", resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	if err != nil {
		return nil, fmt.Errorf("не удалось прочитать спецификацию: %w", err)
	}
	if len(data) > maxBodyBytes {
		return nil, fmt.Errorf("спецификация превышает лимит %d байт", maxBodyBytes)
	}
	return data, nil
}
