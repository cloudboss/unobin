package core

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

// HTTPAction issues an HTTP request and captures the response.
type HTTPAction struct {
	URL     string            `mapstructure:"url"`
	Method  string            `mapstructure:"method"`
	Headers map[string]string `mapstructure:"headers"`
	Body    string            `mapstructure:"body"`
	Timeout time.Duration     `mapstructure:"timeout"`
}

// HTTPResult is the captured response. The action returns an error only
// when the request can't be built or the transport fails, not on HTTP
// error status codes. HTTP status codes are returned as data in Status.
type HTTPResult struct {
	Status     int                 `mapstructure:"status"`
	StatusText string              `mapstructure:"status-text"`
	Headers    map[string][]string `mapstructure:"headers"`
	Body       string              `mapstructure:"body"`
	Duration   time.Duration       `mapstructure:"duration"`
}

// Run issues the request. Method defaults to GET. Timeout applies to the
// whole round trip including reading the response body.
func (a *HTTPAction) Run(ctx context.Context, _ any) (any, error) {
	if a.URL == "" {
		return nil, errors.New("url is required")
	}
	method := a.Method
	if method == "" {
		method = http.MethodGet
	}
	var body io.Reader
	if a.Body != "" {
		body = strings.NewReader(a.Body)
	}
	req, err := http.NewRequestWithContext(ctx, method, a.URL, body)
	if err != nil {
		return nil, err
	}
	for k, v := range a.Headers {
		req.Header.Set(k, v)
	}

	client := &http.Client{}
	if a.Timeout > 0 {
		client.Timeout = a.Timeout
	}

	start := time.Now()
	resp, err := client.Do(req)
	duration := time.Since(start)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return HTTPResult{
		Status:     resp.StatusCode,
		StatusText: resp.Status,
		Headers:    resp.Header,
		Body:       string(bodyBytes),
		Duration:   duration,
	}, nil
}
