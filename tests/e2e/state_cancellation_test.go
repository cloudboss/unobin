package e2e

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/cloudboss/unobin/pkg/e2etest"
)

func TestStateLockCancellation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipped: spawns go build")
	}
	var mu sync.Mutex
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.Method)
		mu.Unlock()
		w.Header().Set("ETag", `"test"`)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	passed := t.Run("compiled", func(t *testing.T) {
		e2etest.RunCompiledCases(t, "testdata/ub/valid/state-lock-cancellation",
			e2etest.WithUnobinDir(filepath.Join("..", "..")),
			e2etest.WithGoModule("example.com/unobin/e2elib", "testdata/modules/e2elib"),
			e2etest.WithEnv(map[string]string{
				"AWS_ACCESS_KEY_ID": "test", "AWS_SECRET_ACCESS_KEY": "test",
				"AWS_SESSION_TOKEN": "", "AWS_EC2_METADATA_DISABLED": "true",
				"AWS_PROFILE": "", "AWS_DEFAULT_PROFILE": "",
				"AWS_CONFIG_FILE": "missing-config", "AWS_SHARED_CREDENTIALS_FILE": "missing-creds",
				"AWS_ENDPOINT_URL_S3": server.URL,
			}),
		)
	})
	if !passed {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	assert.Nil(t, requests)
}
