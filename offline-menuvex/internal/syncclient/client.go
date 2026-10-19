// Package syncclient implements Coffish sync v2 with durable operation replay.
// Writes use supported domain operations, never generic REST replay.
package syncclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/sinaqaffari/coffish/desktop/internal/store"
)

type Client struct {
	BaseURL, DeviceID, DeviceToken string
	HTTP                           *http.Client
	mu                             sync.Mutex
}

type APIError struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	RetryClass string `json:"retry_class"`
}

func (e *APIError) Error() string { return e.Code + ": " + e.Message + " (" + e.RetryClass + ")" }

func New(base, id, token string) (*Client, error) {
	u, err := url.Parse(strings.TrimRight(base, "/"))
	if err != nil {
		return nil, err
	}
	if u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" {
		return nil, errors.New("API URL must be an origin without path, credentials, query or fragment")
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1")) {
		return nil, errors.New("remote API requires HTTPS")
	}
	if strings.ContainsAny(id+token, "\r\n") || id == "" || token == "" {
		return nil, errors.New("device ID and token are required")
	}
	return &Client{
		BaseURL:     u.String(),
		DeviceID:    id,
		DeviceToken: token,
		HTTP: &http.Client{
			Timeout:       30 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}, nil
}

func (c *Client) request(ctx context.Context, method, path string, body any, out any) error {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+"/api/v1/sync"+path, r)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Device "+c.DeviceID+":"+c.DeviceToken)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return errors.New("sync server unreachable; local data remains available")
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, (32<<20)+1))
	if err != nil {
		return err
	}
	if len(raw) > 32<<20 {
		return errors.New("sync response exceeds 32 MiB; local state was not changed")
	}
	var envelope struct {
		Success bool            `json:"success"`
		Data    json.RawMessage `json:"data"`
		Error   *APIError       `json:"error"`
	}
	if err = json.Unmarshal(raw, &envelope); err != nil {
		return fmt.Errorf("sync returned invalid JSON (HTTP %d)", res.StatusCode)
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 || !envelope.Success {
		if envelope.Error != nil {
			return envelope.Error
		}
		return fmt.Errorf("sync failed (HTTP %d)", res.StatusCode)
	}
	if out != nil {
		return json.Unmarshal(envelope.Data, out)
	}
	return nil
}

func (c *Client) bootstrap(ctx context.Context, s *store.Store) error {
	var response struct {
		Cursor  string                       `json:"snapshot_cursor"`
		Data    map[string][]json.RawMessage `json:"data"`
		HasMore bool                         `json:"has_more"`
	}
	sections := []string{"categories", "products", "ingredients", "banners", "recipes", "orders"}
	if err := c.request(ctx, "POST", "/bootstrap", map[string]any{"include": sections}, &response); err != nil {
		return err
	}
	if response.HasMore {
		return errors.New("paged bootstrap not supported; local data not replaced")
	}
	if response.Data == nil {
		return errors.New("bootstrap missing data; local data not replaced")
	}
	return s.Bootstrap(ctx, response.Data, response.Cursor)
}

// Refresh serializes all device sync. Commit data and cursor BEFORE acknowledging.
func (c *Client) Refresh(ctx context.Context, s *store.Store) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := s.Bind(ctx, c.BaseURL+"|"+c.DeviceID); err != nil {
		return err
	}
	cursor, err := s.Cursor(ctx)
	if err != nil {
		return err
	}
	var handshake struct {
		Bootstrap bool `json:"bootstrap_required"`
		Version   int  `json:"supported_protocol_version"`
		Limits    struct {
			MaxBatch int `json:"max_batch_size"`
		} `json:"limits"`
	}
	if err = c.request(ctx, "POST", "/handshake", map[string]any{"sync_protocol_version": 2, "app_version": "desktop-foundation-0.1", "last_known_cursor": cursor}, &handshake); err != nil {
		return err
	}
	if handshake.Version != 2 {
		return errors.New("server does not support sync protocol v2")
	}
	if handshake.Bootstrap || cursor == "" {
		if err = c.bootstrap(ctx, s); err != nil {
			return err
		}
		cursor, err = s.Cursor(ctx)
		if err != nil {
			return err
		}
	}
	// One bounded push batch per refresh; pull still runs after domain conflicts.
	ops, err := s.Outbox(ctx, true)
	if err != nil {
		return err
	}
	if handshake.Limits.MaxBatch > 0 && len(ops) > handshake.Limits.MaxBatch {
		ops = ops[:handshake.Limits.MaxBatch]
	}
	if len(ops) > 0 {
		var response struct {
			Results []store.Result `json:"results"`
		}
		if err = c.request(ctx, "POST", "/push", map[string]any{"operations": ops}, &response); err != nil {
			return err
		}
		if err = s.SaveResults(ctx, ops, response.Results); err != nil {
			return err
		}
	}
	recovered := false
	for page := 0; page < 100; page++ {
		var result struct {
			Next    string         `json:"next_cursor"`
			HasMore bool           `json:"has_more"`
			Changes []store.Change `json:"changes"`
		}
		err = c.request(ctx, "GET", "/pull?cursor="+url.QueryEscape(cursor)+"&limit=500", nil, &result)
		if err != nil {
			var apiErr *APIError
			if !recovered && errors.As(err, &apiErr) && apiErr.RetryClass == "bootstrap" {
				if err = c.bootstrap(ctx, s); err != nil {
					return err
				}
				cursor, err = s.Cursor(ctx)
				if err != nil {
					return err
				}
				recovered = true
				continue
			}
			return err
		}
		if result.HasMore && result.Next == cursor {
			return errors.New("sync cursor did not advance")
		}
		if err = s.ApplyPage(ctx, result.Changes, result.Next); err != nil {
			return err
		}
		cursor = result.Next
		if err = c.request(ctx, "POST", "/ack", map[string]string{"cursor": cursor}, nil); err != nil {
			return err
		}
		if !result.HasMore {
			return nil
		}
	}
	return errors.New("sync page limit reached; refresh again to continue")
}
