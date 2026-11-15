package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const (
	apiRoot   = "/rest/api/3"
	agileRoot = "/rest/agile/1.0"
	userAgent = "pegg-kanban-jira-plugin"
)

type APIError struct {
	Method string
	Path   string
	Status int
	Body   string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("%s %s: HTTP %d: %s", e.Method, e.Path, e.Status, e.Body)
}

type Client struct {
	baseURL string
	email   string
	token   string
	http    *http.Client
}

func basicAuth(email, token string) string {
	if token == "" {
		return ""
	}
	return base64.StdEncoding.EncodeToString([]byte(email + ":" + token))
}

func newClient(s Settings) *Client {
	return &Client{
		baseURL: strings.TrimRight(s.BaseURL(), "/"),
		email:   strings.TrimSpace(s.Email),
		token:   strings.TrimSpace(s.Token),
		http:    &http.Client{Timeout: s.timeout()},
	}
}

func (c *Client) api(path string) string {
	if strings.HasPrefix(path, apiRoot) {
		return path
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return apiRoot + path
}

func (c *Client) agile(path string) string {
	if strings.HasPrefix(path, agileRoot) {
		return path
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return agileRoot + path
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, payload any, out any) error {
	if strings.TrimSpace(c.baseURL) == "" {
		return fmt.Errorf("jira is not configured: set PEGG_JIRA_SITE or plugins.options.jira.site")
	}
	if strings.TrimSpace(c.token) == "" {
		return fmt.Errorf("jira credentials are not configured: set PEGG_JIRA_EMAIL and PEGG_JIRA_TOKEN")
	}
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
		body = bytes.NewReader(encoded)
	}
	return c.doRaw(ctx, method, path, query, body, "", nil, out)
}

func (c *Client) doRaw(ctx context.Context, method, path string, query url.Values, body io.Reader, contentType string, headers map[string]string, out any) error {
	target := c.baseURL + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return c.redactError(fmt.Errorf("build request: %w", err))
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("User-Agent", userAgent)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Basic "+basicAuth(c.email, c.token))
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return c.redactError(fmt.Errorf("%s %s: %w", method, c.redactPath(path), err))
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return c.redactError(fmt.Errorf("read response: %w", err))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(data))
		if len(msg) > 800 {
			msg = msg[:800] + "..."
		}
		msg = c.redact(string(msg))
		return &APIError{Method: method, Path: c.redactPath(path), Status: resp.StatusCode, Body: msg}
	}
	if out == nil || len(data) == 0 {
		return nil
	}
	if raw, ok := out.(*[]byte); ok {
		*raw = data
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		preview := strings.TrimSpace(string(data))
		if len(preview) > 200 {
			preview = preview[:200] + "..."
		}
		return c.redactError(fmt.Errorf("decode response from %s: %w (%s)", c.redactPath(path), err, preview))
	}
	return nil
}

func (c *Client) getJSON(ctx context.Context, path string, query url.Values, out any) error {
	return c.do(ctx, http.MethodGet, path, query, nil, out)
}

func (c *Client) postJSON(ctx context.Context, path string, payload any, out any) error {
	return c.do(ctx, http.MethodPost, path, nil, payload, out)
}

func (c *Client) putJSON(ctx context.Context, path string, payload any, out any) error {
	return c.do(ctx, http.MethodPut, path, nil, payload, out)
}

func (c *Client) redact(value string) string {
	for _, secret := range []string{c.token, basicAuth(c.email, c.token)} {
		if secret != "" {
			value = strings.ReplaceAll(value, secret, "[redacted]")
		}
	}
	return value
}

func (c *Client) redactPath(path string) string {
	return c.redact(path)
}

func (c *Client) redactError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s", c.redact(err.Error()))
}

func isHTTPStatus(err error, statuses ...int) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	for _, status := range statuses {
		if apiErr.Status == status {
			return true
		}
	}
	return false
}

func truncateOutput(value string, limit int) string {
	if limit <= 0 {
		limit = 60 << 10
	}
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "\n... (truncated)"
}
