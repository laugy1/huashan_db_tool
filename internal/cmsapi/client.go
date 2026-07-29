// Package cmsapi talks to the DCSN backend (login + media upload).
package cmsapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

// Client calls DCSN login and media upload endpoints.
type Client struct {
	baseURL    string
	username   string
	password   string
	httpClient *http.Client
	token      string
}

// NewClient creates a client. baseURL is e.g. http://localhost:9453/dcsn (no trailing slash).
func NewClient(baseURL, username, password string) *Client {
	return &Client{
		baseURL:  strings.TrimRight(baseURL, "/"),
		username: username,
		password: password,
		httpClient: &http.Client{
			Timeout: 5 * time.Minute,
		},
	}
}

// Login authenticates and stores the bearer token for subsequent uploads.
func (c *Client) Login(ctx context.Context) error {
	form := url.Values{}
	form.Set("username", c.username)
	form.Set("password", c.password)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/login", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("login request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("login read body: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("login HTTP %d: %s", resp.StatusCode, truncate(string(body), 512))
	}

	token, err := extractToken(body)
	if err != nil {
		return err
	}
	c.token = token
	return nil
}

// Upload sends image bytes to /media/upload and returns metaList entries.
func (c *Client) Upload(ctx context.Context, filename string, data []byte) ([]map[string]any, error) {
	if c.token == "" {
		return nil, fmt.Errorf("not logged in")
	}

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("file", filename)
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(data); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/media/upload", &buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("upload request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("upload read body: %w", err)
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("upload unauthorized (token expired?)")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("upload HTTP %d: %s", resp.StatusCode, truncate(string(body), 512))
	}

	var out uploadResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("upload parse json: %w", err)
	}
	if len(out.MetaList) == 0 {
		return nil, fmt.Errorf("upload: empty metaList in response")
	}
	return out.MetaList, nil
}

type uploadResponse struct {
	Message  string             `json:"message"`
	MetaList []map[string]any   `json:"metaList"`
}

func extractToken(body []byte) (string, error) {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		// 部分後端只回傳純文字 token
		s := strings.TrimSpace(string(body))
		if s != "" && !strings.HasPrefix(s, "{") {
			return s, nil
		}
		return "", fmt.Errorf("login parse json: %w", err)
	}
	if t := findToken(m); t != "" {
		return t, nil
	}
	return "", fmt.Errorf("login: token not found in response: %s", truncate(string(body), 256))
}

func findToken(v any) string {
	switch x := v.(type) {
	case map[string]any:
		for _, key := range []string{"token", "access_token", "accessToken", "jwt", "bearer"} {
			if raw, ok := x[key]; ok {
				if s, ok := raw.(string); ok && s != "" {
					return s
				}
			}
		}
		for _, nested := range x {
			if t := findToken(nested); t != "" {
				return t
			}
		}
	case []any:
		for _, item := range x {
			if t := findToken(item); t != "" {
				return t
			}
		}
	}
	return ""
}

// Download fetches a remote image URL.
func Download(ctx context.Context, imageURL string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil {
		return nil, "", err
	}
	client := &http.Client{Timeout: 2 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("download HTTP %d for %s", resp.StatusCode, imageURL)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 50<<20))
	if err != nil {
		return nil, "", err
	}
	filename := path.Base(imageURL)
	if idx := strings.Index(filename, "?"); idx >= 0 {
		filename = filename[:idx]
	}
	if filename == "" || filename == "." || filename == "/" {
		filename = "image.jpg"
	}
	return data, filename, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
