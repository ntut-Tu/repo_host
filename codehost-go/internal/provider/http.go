package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var ErrIncomplete = errors.New("upstream resource is incomplete")
var ErrUnavailable = errors.New("upstream resource is unavailable")

type transport struct {
	client   *http.Client
	apiURL   string
	token    string
	provider string
}

func newTransport(apiURL, token, provider string) transport {
	client := &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
		return errors.New("upstream redirect is not allowed")
	}}
	return transport{client: client, apiURL: strings.TrimRight(apiURL, "/"),
		token: token, provider: provider}
}

func (t transport) request(ctx context.Context, method, target string, body any, result any) (http.Header, error) {
	var payload io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		payload = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, payload)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if t.token != "" {
		if t.provider == "github" {
			req.Header.Set("Authorization", "Bearer "+t.token)
		} else {
			req.Header.Set("PRIVATE-TOKEN", t.token)
		}
	}
	response, err := t.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: request failed", ErrUnavailable)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("%w: HTTP %d", ErrUnavailable, response.StatusCode)
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 10<<20))
	if err := decoder.Decode(result); err != nil {
		return nil, fmt.Errorf("%w: invalid JSON", ErrIncomplete)
	}
	return response.Header, nil
}

func (t transport) query(ctx context.Context, endpoint, query string, variables any, target any) error {
	var envelope struct {
		Data   json.RawMessage `json:"data"`
		Errors json.RawMessage `json:"errors"`
	}
	_, err := t.request(ctx, http.MethodPost, endpoint, map[string]any{
		"query": query, "variables": variables,
	}, &envelope)
	if err != nil {
		return err
	}
	if len(envelope.Errors) > 0 && string(envelope.Errors) != "null" && string(envelope.Errors) != "[]" {
		return ErrIncomplete
	}
	if len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return ErrIncomplete
	}
	return json.Unmarshal(envelope.Data, target)
}

func (t transport) pages(ctx context.Context, path string) ([]map[string]any, error) {
	all := make([]map[string]any, 0)
	for page := 1; ; page++ {
		if page > 1000 {
			return nil, ErrIncomplete
		}
		separator := "?"
		if strings.Contains(path, "?") {
			separator = "&"
		}
		target := t.apiURL + path + separator + "per_page=100&page=" + strconv.Itoa(page)
		var items []map[string]any
		header, err := t.request(ctx, http.MethodGet, target, nil, &items)
		if err != nil {
			return nil, err
		}
		all = append(all, items...)
		next := header.Get("X-Next-Page")
		if next == "" {
			return all, nil
		}
		if next != strconv.Itoa(page+1) {
			return nil, ErrIncomplete
		}
	}
}

func escapeProject(repository string) string { return url.PathEscape(repository) }

func asInt(value any) (int, bool) {
	switch v := value.(type) {
	case float64:
		return int(v), v == float64(int(v))
	case int:
		return v, true
	default:
		return 0, false
	}
}
