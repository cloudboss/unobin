package core

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/cloudboss/unobin/pkg/defaults"
)

// HTTPAction issues an HTTP request and captures the response.
type HTTPAction struct {
	URL     string
	Method  string
	Headers map[string]string
	Body    string
	Timeout time.Duration
}

// Defaults declares the inputs a body may leave out: the method
// defaults to GET; absent headers and body send nothing extra, and an
// absent timeout leaves the round trip unbounded.
func (a HTTPAction) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.Value(a.Method, "GET"),
		defaults.Optional(a.Headers),
		defaults.Optional(a.Body),
		defaults.Optional(a.Timeout),
	}
}

// HTTPActionOutput is the captured response. The action returns an error only
// when the request can't be built or the transport fails, not on HTTP
// error status codes. HTTP status codes are returned as data in Status.
type HTTPActionOutput struct {
	Status     int
	StatusText string
	Headers    map[string][]string
	Body       string
	Duration   time.Duration
}

// Run issues the request. Method defaults to GET. Timeout applies to the
// whole round trip including reading the response body.
func (a *HTTPAction) Run(ctx context.Context, _ any) (*HTTPActionOutput, error) {
	if a.URL == "" {
		return nil, errors.New("url is required")
	}
	var body io.Reader
	if a.Body != "" {
		body = strings.NewReader(a.Body)
	}
	req, err := http.NewRequestWithContext(ctx, a.Method, a.URL, body)
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

	return &HTTPActionOutput{
		Status:     resp.StatusCode,
		StatusText: resp.Status,
		Headers:    resp.Header,
		Body:       string(bodyBytes),
		Duration:   duration,
	}, nil
}
