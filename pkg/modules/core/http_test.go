package core

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func runHTTP(t *testing.T, a *HTTPAction) *HTTPActionOutput {
	t.Helper()
	res, err := a.Run(context.Background(), nil)
	require.NoError(t, err)
	require.NotNil(t, res)
	return res
}

func TestHTTPGet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		w.Header().Set("X-Test", "yes")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "hello")
	}))
	defer srv.Close()

	hr := runHTTP(t, &HTTPAction{URL: srv.URL})
	require.Equal(t, 200, hr.Status)
	require.Equal(t, "hello", hr.Body)
	require.Equal(t, []string{"yes"}, hr.Headers["X-Test"])
	require.True(t, hr.Duration > 0)
}

func TestHTTPPostWithHeadersAndBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "Bearer xyz", r.Header.Get("Authorization"))
		body, _ := io.ReadAll(r.Body)
		require.Equal(t, `{"k":"v"}`, string(body))
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	hr := runHTTP(t, &HTTPAction{
		URL:    srv.URL,
		Method: http.MethodPost,
		Headers: map[string]string{
			"Authorization": "Bearer xyz",
			"Content-Type":  "application/json",
		},
		Body: `{"k":"v"}`,
	})
	require.Equal(t, 201, hr.Status)
}

func TestHTTPReports404AsData(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "missing", http.StatusNotFound)
	}))
	defer srv.Close()

	hr := runHTTP(t, &HTTPAction{URL: srv.URL})
	require.Equal(t, 404, hr.Status)
	require.Contains(t, hr.Body, "missing")
}

func TestHTTPRequiresURL(t *testing.T) {
	_, err := (&HTTPAction{}).Run(context.Background(), nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "url is required")
}

func TestHTTPTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	a := &HTTPAction{URL: srv.URL, Timeout: 10 * time.Millisecond}
	_, err := a.Run(context.Background(), nil)
	require.Error(t, err)
}

func TestHTTPContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, err := (&HTTPAction{URL: srv.URL}).Run(ctx, nil)
	require.Error(t, err)
}

func TestCoreModuleRegistersHTTP(t *testing.T) {
	at, ok := Module().Actions["http"]
	require.True(t, ok)
	require.NotNil(t, at)
	_, ok = at.NewReceiver().(*HTTPAction)
	require.True(t, ok)
}
