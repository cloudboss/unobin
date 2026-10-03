package e2e

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/e2etest"
)

func TestStateBucketBootstrap(t *testing.T) {
	if testing.Short() {
		t.Skip("skipped: spawns go build")
	}
	var mu sync.Mutex
	created := false
	var bucketRequests []string
	objects := map[string][]byte{}
	var locks, unlocks int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		_, key, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
		if key == "" {
			bucketRequests = append(bucketRequests, r.Method+" "+r.URL.RawQuery)
			if r.Method == http.MethodHead && !created {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			if r.Method == http.MethodPut && r.URL.RawQuery == "" {
				assert.False(t, created, "an existing bucket must not be created again")
				created = true
			} else if r.Method == http.MethodPut {
				assert.True(t, created)
				assert.True(t, r.URL.Query().Has("tagging"))
			}
			w.WriteHeader(http.StatusOK)
			return
		}
		if !assert.True(t, created, "bucket initialization must finish before object requests") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		switch r.Method {
		case http.MethodGet:
			body, ok := objects[key]
			if !ok {
				w.Header().Set("Content-Type", "application/xml")
				w.WriteHeader(http.StatusNotFound)
				fmt.Fprint(w, "<Error><Code>NoSuchKey</Code></Error>")
				return
			}
			checksum := sha256.Sum256(body)
			w.Header().Set("x-amz-checksum-sha256", base64.StdEncoding.EncodeToString(checksum[:]))
			_, _ = w.Write(body)
		case http.MethodHead:
			body, ok := objects[key]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Length", fmt.Sprint(len(body)))
		case http.MethodPut:
			if r.Header.Get("If-None-Match") == "*" {
				if _, ok := objects[key]; ok {
					w.WriteHeader(http.StatusPreconditionFailed)
					return
				}
				if strings.HasSuffix(key, "/lock") {
					locks++
				}
			}
			body, err := io.ReadAll(r.Body)
			assert.NoError(t, err)
			objects[key] = body
			w.Header().Set("ETag", `"test"`)
		case http.MethodDelete:
			delete(objects, key)
			if strings.HasSuffix(key, "/lock") {
				unlocks++
			}
		default:
			w.WriteHeader(http.StatusNotImplemented)
		}
	}))
	defer server.Close()
	passed := t.Run("compiled", func(t *testing.T) {
		e2etest.RunCompiledCases(t, "testdata/ub/valid/state-bootstrap",
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
	assert.True(t, created)
	assert.Equal(t, 1, locks)
	assert.Equal(t, 1, unlocks)
	assert.Equal(t, []string{"HEAD ", "PUT ", "HEAD ", "PUT tagging=", "HEAD ", "HEAD ",
		"HEAD "}, bucketRequests)
	var currentKey, snapshotKey string
	var keys []string
	for key := range objects {
		keys = append(keys, key)
		if strings.HasSuffix(key, "/current") {
			currentKey = key
			snapshotKey = strings.TrimSuffix(key, "current") + "snapshots/" +
				strings.TrimSpace(string(objects[key])) + ".json.enc"
		}
	}
	require.NotEmpty(t, currentKey)
	assert.Contains(t, keys, snapshotKey)
	for _, key := range keys {
		if key != currentKey {
			assert.True(t, strings.HasPrefix(key, strings.TrimSuffix(currentKey, "current")+
				"snapshots/"), "unexpected object %q", key)
		}
	}
}
