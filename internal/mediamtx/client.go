package mediamtx

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client — клиент HTTP API MediaMTX.
type Client struct {
	baseURL string
	http    *http.Client
}

// NewClient создает клиент MediaMTX.
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		http:    &http.Client{Timeout: 10 * time.Second},
	}
}

// pathConfig описывает конфигурацию пути MediaMTX.
type pathConfig struct {
	Source         string `json:"source"`
	SourceOnDemand *bool  `json:"sourceOnDemand,omitempty"`
}

// Ping проверяет доступность API MediaMTX.
func (c *Client) Ping(ctx context.Context) error {
	res, body, err := c.do(ctx, http.MethodGet, "/v3/config/global/get", nil)
	if err != nil {
		return err
	}
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("mediamtx api ping: status %d, body: %s", res.StatusCode, string(body))
	}
	return nil
}

// AddPath регистрирует путь с source-on-demand для камеры.
func (c *Client) AddPath(ctx context.Context, name string, sourceRTSP string) error {
	sourceOnDemand := true
	cfg := pathConfig{
		Source:         sourceRTSP,
		SourceOnDemand: &sourceOnDemand,
	}

	payload, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal path config: %w", err)
	}

	res, body, err := c.do(ctx, http.MethodPost, "/v3/config/paths/add/"+name, payload)
	if err != nil {
		return err
	}

	if res.StatusCode == http.StatusOK || res.StatusCode == http.StatusCreated || res.StatusCode == http.StatusNoContent {
		return nil
	}

	// Путь уже существует — обновляем конфигурацию.
	if res.StatusCode == http.StatusBadRequest || res.StatusCode == http.StatusConflict {
		return c.EditPath(ctx, name, sourceRTSP)
	}

	return fmt.Errorf("mediamtx add path %s: status %d, body: %s", name, res.StatusCode, string(body))
}

// EditPath обновляет конфигурацию существующего пути.
func (c *Client) EditPath(ctx context.Context, name string, sourceRTSP string) error {
	sourceOnDemand := true
	cfg := pathConfig{
		Source:         sourceRTSP,
		SourceOnDemand: &sourceOnDemand,
	}

	payload, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal path config: %w", err)
	}

	res, body, err := c.do(ctx, http.MethodPatch, "/v3/config/paths/edit/"+name, payload)
	if err != nil {
		return err
	}
	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusNoContent {
		return fmt.Errorf("mediamtx edit path %s: status %d, body: %s", name, res.StatusCode, string(body))
	}
	return nil
}

// RemovePath удаляет путь. Отсутствие пути не считается ошибкой.
func (c *Client) RemovePath(ctx context.Context, name string) error {
	res, body, err := c.do(ctx, http.MethodPost, "/v3/config/paths/remove/"+name, nil)
	if err != nil {
		return err
	}
	if res.StatusCode == http.StatusNotFound {
		return nil
	}
	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusNoContent {
		return fmt.Errorf("mediamtx remove path %s: status %d, body: %s", name, res.StatusCode, string(body))
	}
	return nil
}

func (c *Client) do(ctx context.Context, method, path string, body []byte) (*http.Response, []byte, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return nil, nil, fmt.Errorf("create request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	res, err := c.http.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("mediamtx request %s %s: %w", method, path, err)
	}
	defer res.Body.Close()

	resBody, _ := io.ReadAll(res.Body)
	return res, resBody, nil
}