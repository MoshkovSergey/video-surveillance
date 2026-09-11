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

// PathInfo описывает runtime-состояние пути MediaMTX.
type PathInfo struct {
	Name  string `json:"name"`
	Ready bool   `json:"ready"`
}

type pathsListResponse struct {
	Items []PathInfo `json:"items"`
}

// pathConfig описывает конфигурацию пути MediaMTX.
type pathConfig struct {
	Source                string `json:"source"`
	SourceOnDemand        *bool  `json:"sourceOnDemand,omitempty"`
	Record                *bool  `json:"record,omitempty"`
	RecordPath            string `json:"recordPath,omitempty"`
	RecordSegmentDuration string `json:"recordSegmentDuration,omitempty"`
}

func newPathConfig(sourceRTSP string) pathConfig {
	sourceOnDemand := false
	record := true

	return pathConfig{
		Source:                sourceRTSP,
		SourceOnDemand:        &sourceOnDemand,
		Record:                &record,
		RecordPath:            "/recordings/%path/%Y-%m-%d_%H-%M-%S",
		RecordSegmentDuration: "60s",
	}
}

// removeVariants — кандидатные эндпоинты удаления пути в разных версиях MediaMTX.
var removeVariants = []struct {
	method string
	path   string
}{
	{http.MethodPost, "/v3/config/paths/remove/"},
	{http.MethodDelete, "/v3/config/paths/remove/"},
	{http.MethodPost, "/v3/config/paths/delete/"},
	{http.MethodDelete, "/v3/config/paths/delete/"},
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

// ListPaths возвращает runtime-состояние всех путей MediaMTX.
func (c *Client) ListPaths(ctx context.Context) ([]PathInfo, error) {
	res, body, err := c.do(ctx, http.MethodGet, "/v3/paths/list", nil)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("mediamtx paths list: status %d, body: %s", res.StatusCode, string(body))
	}

	var parsed pathsListResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("unmarshal paths list: %w", err)
	}
	return parsed.Items, nil
}

// pathConfigGet возвращает текущую конфигурацию пути; ok=false, если пути нет.
func (c *Client) pathConfigGet(ctx context.Context, name string) (*pathConfig, bool) {
	res, body, err := c.do(ctx, http.MethodGet, "/v3/config/paths/get/"+name, nil)
	if err != nil || res.StatusCode != http.StatusOK {
		return nil, false
	}

	var cfg pathConfig
	if err := json.Unmarshal(body, &cfg); err != nil {
		return nil, false
	}
	return &cfg, true
}

// AddPath регистрирует путь камеры с непрерывной записью.
// Если путь уже существует и его source совпадает — операция считается успешной.
// Если source отличается — путь удаляется и создаётся заново.
func (c *Client) AddPath(ctx context.Context, name string, sourceRTSP string) error {
	payload, err := json.Marshal(newPathConfig(sourceRTSP))
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

	if res.StatusCode == http.StatusBadRequest || res.StatusCode == http.StatusConflict {
		// Путь уже существует: проверяем, требуется ли обновление.
		if cfg, ok := c.pathConfigGet(ctx, name); ok && cfg.Source == sourceRTSP {
			return nil
		}

		if err := c.RemovePath(ctx, name); err != nil {
			return err
		}

		res, body, err = c.do(ctx, http.MethodPost, "/v3/config/paths/add/"+name, payload)
		if err != nil {
			return err
		}
		if res.StatusCode == http.StatusOK || res.StatusCode == http.StatusCreated || res.StatusCode == http.StatusNoContent {
			return nil
		}
		return fmt.Errorf("mediamtx re-add path %s: status %d, body: %s", name, res.StatusCode, string(body))
	}

	return fmt.Errorf("mediamtx add path %s: status %d, body: %s", name, res.StatusCode, string(body))
}

// RemovePath удаляет путь, перебирая эндпоинты разных версий MediaMTX.
// Отсутствие пути не считается ошибкой.
func (c *Client) RemovePath(ctx context.Context, name string) error {
	if _, ok := c.pathConfigGet(ctx, name); !ok {
		return nil
	}

	for _, variant := range removeVariants {
		res, _, err := c.do(ctx, variant.method, variant.path+name, nil)
		if err != nil {
			continue
		}
		if res.StatusCode == http.StatusOK || res.StatusCode == http.StatusNoContent {
			if _, still := c.pathConfigGet(ctx, name); !still {
				return nil
			}
		}
	}

	// Контрольная проверка: возможно, удаление всё же состоялось.
	if _, still := c.pathConfigGet(ctx, name); !still {
		return nil
	}

	return fmt.Errorf("mediamtx remove path %s: no working remove endpoint in this MediaMTX version", name)
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