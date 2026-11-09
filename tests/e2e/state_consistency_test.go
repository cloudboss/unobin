package e2e

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/e2etest"
	"github.com/cloudboss/unobin/pkg/encrypters"
	"github.com/cloudboss/unobin/pkg/sdk/state"
)

func TestPlanSnapshotConsistency(t *testing.T) {
	if testing.Short() {
		t.Skip("skipped: spawns go build")
	}
	const (
		before = "2026-01-01T00:00:00Z"
		after  = "2026-01-01T00:00:01Z"
		prefix = "state/state-snapshot-consistency/s3/"
	)
	factory := state.FactoryInfo{Name: "state-snapshot-consistency", Version: "v0.0.0"}
	snapshot := state.NewSnapshot(factory, "s3")
	seal := func() []byte {
		body, err := state.EncodeSnapshot(snapshot)
		require.NoError(t, err)
		sealed, err := state.Seal(body, state.PayloadTypeState, encrypters.Noop{})
		require.NoError(t, err)
		return sealed
	}
	objects := map[string][]byte{
		prefix + "current":                           []byte(before + "\n"),
		prefix + "snapshots/" + before + ".json.enc": seal(),
	}
	snapshot.Entries = []*state.Entry{{
		Address: "resource.concurrent", Category: "resource",
		Binding: &state.Binding{Alias: "test", Export: "file"},
		Inputs:  map[string]any{"path": "concurrent.txt"},
		Outputs: map[string]any{"path": "concurrent.txt"},
	}}
	objects[prefix+"snapshots/"+after+".json.enc"] = seal()
	expected := maps.Clone(objects)
	expected[prefix+"current"] = []byte(after + "\n")
	var mu sync.Mutex
	advanced := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		_, key, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
		if key == "" && r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		switch r.Method {
		case http.MethodGet:
			body, exists := objects[key]
			if !exists {
				w.Header().Set("Content-Type", "application/xml")
				w.WriteHeader(http.StatusNotFound)
				fmt.Fprint(w, "<Error><Code>NoSuchKey</Code></Error>")
				return
			}
			if key == prefix+"snapshots/"+before+".json.enc" {
				objects[prefix+"current"] = []byte(after + "\n")
				advanced = true
			}
			checksum := sha256.Sum256(body)
			w.Header().Set("x-amz-checksum-sha256", base64.StdEncoding.EncodeToString(checksum[:]))
			_, _ = w.Write(body)
		case http.MethodHead:
			body, exists := objects[key]
			if !exists {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Length", fmt.Sprint(len(body)))
		case http.MethodPut:
			if r.Header.Get("If-None-Match") == "*" {
				if _, exists := objects[key]; exists {
					w.WriteHeader(http.StatusPreconditionFailed)
					return
				}
			}
			body, err := io.ReadAll(r.Body)
			assert.NoError(t, err)
			objects[key] = body
			w.Header().Set("ETag", `"test"`)
		case http.MethodDelete:
			delete(objects, key)
		default:
			w.WriteHeader(http.StatusNotImplemented)
		}
	}))
	defer server.Close()
	passed := t.Run("compiled", func(t *testing.T) {
		e2etest.RunCompiledCases(t, "testdata/ub/valid/state-snapshot-consistency",
			e2etest.WithUnobinDir(filepath.Join("..", "..")),
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
	assert.True(t, advanced)
	assert.Equal(t, expected, objects)
}
