package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// The only state: object "last-sent" in STATE_BUCKET holding the date
// (YYYY-MM-DD) of the last successful push. Plain GCS JSON API, no SDK.
const stateObject = "last-sent"

// gcsToken returns an OAuth token: GOOGLE_OAUTH_TOKEN if set (local runs:
// `gcloud auth print-access-token`), else the Cloud Run metadata server.
func gcsToken(ctx context.Context, c *http.Client) (string, error) {
	if t := os.Getenv("GOOGLE_OAUTH_TOKEN"); t != "" {
		return t, nil
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		"http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/default/token", nil)
	req.Header.Set("Metadata-Flavor", "Google")
	resp, err := c.Do(req)
	if err != nil {
		return "", fmt.Errorf("metadata token (set GOOGLE_OAUTH_TOKEN when running locally): %w", err)
	}
	defer resp.Body.Close()
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil || tok.AccessToken == "" {
		return "", fmt.Errorf("metadata token: HTTP %d", resp.StatusCode)
	}
	return tok.AccessToken, nil
}

func lastSent(ctx context.Context, c *http.Client, cfg Config) (string, error) {
	tok, err := gcsToken(ctx, c)
	if err != nil {
		return "", err
	}
	u := fmt.Sprintf("https://storage.googleapis.com/storage/v1/b/%s/o/%s?alt=media", cfg.StateBucket, stateObject)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := c.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", nil
	}
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("read state: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return strings.TrimSpace(string(b)), nil
}

func markSent(ctx context.Context, c *http.Client, cfg Config, day string) error {
	tok, err := gcsToken(ctx, c)
	if err != nil {
		return err
	}
	u := fmt.Sprintf("https://storage.googleapis.com/upload/storage/v1/b/%s/o?uploadType=media&name=%s",
		cfg.StateBucket, url.QueryEscape(stateObject))
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(day))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "text/plain")
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("write state: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}
