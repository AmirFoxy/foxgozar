// Package clash talks to the running core through its Clash-compatible API:
// switching node, measuring delays, traffic and connections.
package clash

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

type Client struct {
	Base   string // http://127.0.0.1:9090
	Secret string
	hc     *http.Client
}

func New(port int, secret string) *Client {
	return &Client{
		Base:   fmt.Sprintf("http://127.0.0.1:%d", port),
		Secret: secret,
		hc:     &http.Client{Transport: &http.Transport{Proxy: nil, MaxIdleConns: 4}},
	}
}

func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.Base+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Secret)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("core API %s %s: %d %s", method, path, resp.StatusCode, bytes.TrimSpace(b))
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

func (c *Client) Ping(ctx context.Context) error {
	return c.do(ctx, http.MethodGet, "/version", nil, nil)
}

type Proxy struct {
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	Now     string   `json:"now,omitempty"`
	All     []string `json:"all,omitempty"`
	History []struct {
		Delay int `json:"delay"`
	} `json:"history"`
}

func (p Proxy) LastDelay() int {
	if len(p.History) == 0 {
		return 0
	}
	return p.History[len(p.History)-1].Delay
}

func (c *Client) Proxies(ctx context.Context) (map[string]Proxy, error) {
	var out struct {
		Proxies map[string]Proxy `json:"proxies"`
	}
	err := c.do(ctx, http.MethodGet, "/proxies", nil, &out)
	return out.Proxies, err
}

// Select switches a selector. Takes effect at once; no restart.
func (c *Client) Select(ctx context.Context, group, name string) error {
	return c.do(ctx, http.MethodPut, "/proxies/"+url.PathEscape(group), map[string]string{"name": name}, nil)
}

// Delay measures one node through the running core.
func (c *Client) Delay(ctx context.Context, name, testURL string, timeout time.Duration) (int, error) {
	var out struct {
		Delay int `json:"delay"`
	}
	q := url.Values{"url": {testURL}, "timeout": {fmt.Sprint(timeout.Milliseconds())}}
	err := c.do(ctx, http.MethodGet, "/proxies/"+url.PathEscape(name)+"/delay?"+q.Encode(), nil, &out)
	return out.Delay, err
}

// GroupDelay tests every member of a group at once; the core runs them in
// parallel. Members that failed are absent from the result.
func (c *Client) GroupDelay(ctx context.Context, group, testURL string, timeout time.Duration) (map[string]int, error) {
	out := map[string]int{}
	q := url.Values{"url": {testURL}, "timeout": {fmt.Sprint(timeout.Milliseconds())}}
	err := c.do(ctx, http.MethodGet, "/group/"+url.PathEscape(group)+"/delay?"+q.Encode(), nil, &out)
	return out, err
}

type Connections struct {
	DownloadTotal int64            `json:"downloadTotal"`
	UploadTotal   int64            `json:"uploadTotal"`
	Connections   []map[string]any `json:"connections"`
	Memory        int64            `json:"memory"`
}

func (c *Client) Connections(ctx context.Context) (Connections, error) {
	var out Connections
	err := c.do(ctx, http.MethodGet, "/connections", nil, &out)
	return out, err
}

func (c *Client) CloseAll(ctx context.Context) error {
	return c.do(ctx, http.MethodDelete, "/connections", nil, nil)
}
