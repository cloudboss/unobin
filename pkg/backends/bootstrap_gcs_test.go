package backends

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/api/option"
	"google.golang.org/api/storage/v1"

	"github.com/cloudboss/unobin/pkg/encrypters"
	"github.com/cloudboss/unobin/pkg/gcpcfg"
	sdkstate "github.com/cloudboss/unobin/pkg/sdk/state"
)

func bootstrapGCSService(t *testing.T, endpoint string) *storage.Service {
	t.Helper()
	service, err := storage.NewService(context.Background(),
		option.WithEndpoint(endpoint+"/"), option.WithoutAuthentication())
	require.NoError(t, err)
	return service
}

func TestGCSBootstrapDisabled(t *testing.T) {
	for _, config := range []*GCSBootstrapConfig{nil, {}} {
		require.NoError(t, bootstrapGCSBucket(t.Context(), nil, "state", "", config))
	}
}

func TestGCSBootstrapReusesExistingBucket(t *testing.T) {
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"name":"state","location":"EU","versioning":{"enabled":false}}`)
	}))
	defer server.Close()
	config := &GCSBootstrapConfig{Versioning: &GCSBucketVersioning{Enabled: true}}
	require.NoError(t, bootstrapGCSBucket(t.Context(), bootstrapGCSService(t, server.URL),
		"state", "", config))
	assert.Equal(t, []string{"GET /b/state"}, requests)
}

func TestGCSBootstrapCreatesBucketWithOptions(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		t.Run(fmt.Sprint(enabled), func(t *testing.T) {
			var requests []string
			var project string
			var body map[string]any
			server := httptest.NewServer(http.HandlerFunc(
				func(w http.ResponseWriter, r *http.Request) {
					requests = append(requests, r.Method+" "+r.URL.Path)
					w.Header().Set("Content-Type", "application/json")
					if r.Method == http.MethodGet {
						w.WriteHeader(http.StatusNotFound)
						fmt.Fprint(w, `{"error":{"code":404,"message":"missing"}}`)
						return
					}
					project = r.URL.Query().Get("project")
					assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
					fmt.Fprint(w, `{"name":"state"}`)
				}))
			defer server.Close()
			config := &GCSBootstrapConfig{
				Location:   new("US"),
				Versioning: &GCSBucketVersioning{Enabled: enabled},
				IAMConfiguration: &GCSBucketIAMConfiguration{
					PublicAccessPrevention: new("enforced"),
					UniformBucketLevelAccess: &GCSBucketUniformBucketLevelAccess{
						Enabled: enabled,
					},
				},
				Labels: new(map[string]string{"purpose": "state"}),
			}
			require.NoError(t, bootstrapGCSBucket(t.Context(),
				bootstrapGCSService(t, server.URL), "state", "acme-prod", config))
			assert.Equal(t, []string{"GET /b/state", "POST /b"}, requests)
			assert.Equal(t, "acme-prod", project)
			assert.Equal(t, map[string]any{
				"name": "state", "location": "US",
				"versioning": map[string]any{"enabled": enabled},
				"iamConfiguration": map[string]any{
					"publicAccessPrevention":   "enforced",
					"uniformBucketLevelAccess": map[string]any{"enabled": enabled},
				},
				"labels": map[string]any{"purpose": "state"},
			}, body)
		})
	}
}

func TestGCSBootstrapRequiresCreationLocationAndProject(t *testing.T) {
	tests := []struct {
		name, project, wantError string
		config                   *GCSBootstrapConfig
	}{
		{
			name: "project", config: &GCSBootstrapConfig{Location: new("US")},
			wantError: "gcp.project",
		},
		{
			name: "location", project: "acme-prod",
			config:    &GCSBootstrapConfig{Versioning: &GCSBucketVersioning{}},
			wantError: "bootstrap.location",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var requests []string
			server := httptest.NewServer(http.HandlerFunc(
				func(w http.ResponseWriter, r *http.Request) {
					requests = append(requests, r.Method)
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusNotFound)
					fmt.Fprint(w, `{"error":{"code":404,"message":"missing"}}`)
				}))
			defer server.Close()
			err := bootstrapGCSBucket(t.Context(), bootstrapGCSService(t, server.URL),
				"state", tt.project, tt.config)
			require.ErrorContains(t, err, tt.wantError)
			assert.Equal(t, []string{"GET"}, requests)
		})
	}
}

func TestGCSBootstrapFailuresAndCreationRace(t *testing.T) {
	tests := []struct {
		name       string
		headStatus int
		postStatus int
		lastStatus int
		want       []string
		wantError  string
	}{
		{
			name: "access denied", headStatus: http.StatusForbidden,
			want: []string{"GET"}, wantError: "check bucket",
		},
		{
			name: "another creator", headStatus: http.StatusNotFound,
			postStatus: http.StatusConflict, lastStatus: http.StatusOK,
			want: []string{"GET", "POST", "GET"},
		},
		{
			name: "cannot access conflicting bucket", headStatus: http.StatusNotFound,
			postStatus: http.StatusConflict, lastStatus: http.StatusForbidden,
			want: []string{"GET", "POST", "GET"}, wantError: "check bucket after conflict",
		},
		{
			name: "cannot create", headStatus: http.StatusNotFound,
			postStatus: http.StatusForbidden, want: []string{"GET", "POST"},
			wantError: "create bucket",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var requests []string
			server := httptest.NewServer(http.HandlerFunc(
				func(w http.ResponseWriter, r *http.Request) {
					requests = append(requests, r.Method)
					status := tt.headStatus
					if r.Method == http.MethodPost {
						status = tt.postStatus
					} else if len(requests) > 1 {
						status = tt.lastStatus
					}
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(status)
					if status == http.StatusOK {
						fmt.Fprint(w, `{"name":"state"}`)
					} else {
						fmt.Fprintf(w, `{"error":{"code":%d,"message":"failed"}}`, status)
					}
				}))
			defer server.Close()
			err := bootstrapGCSBucket(t.Context(), bootstrapGCSService(t, server.URL),
				"state", "acme-prod", &GCSBootstrapConfig{Location: new("US")})
			if tt.wantError == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tt.wantError)
			}
			assert.Equal(t, tt.want, requests)
		})
	}
}

func TestNewGCSBackendBootstrapsBeforeStateRequests(t *testing.T) {
	created := false
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/token" {
			fmt.Fprint(w, `{"access_token":"test","token_type":"Bearer","expires_in":3600}`)
			return
		}
		requests = append(requests, r.Method+" "+r.URL.Path)
		if r.Method == http.MethodPost {
			assert.Equal(t, "acme-prod", r.URL.Query().Get("project"))
			var input storage.Bucket
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&input))
			assert.Equal(t, "US", input.Location)
			created = true
			fmt.Fprint(w, `{"name":"state"}`)
			return
		}
		if strings.Contains(r.URL.Path, "/o/") {
			assert.True(t, created)
		} else if created {
			fmt.Fprint(w, `{"name":"state"}`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"error":{"code":404,"message":"missing"}}`)
	}))
	defer server.Close()
	dir := t.TempDir()
	subject := filepath.Join(dir, "subject-token")
	require.NoError(t, os.WriteFile(subject, []byte("test"), 0o600))
	credentials, err := json.Marshal(map[string]any{
		"type":               "external_account",
		"audience":           "//iam.googleapis.com/projects/1/locations/global/pools/p/providers/p",
		"subject_token_type": "urn:ietf:params:oauth:token-type:jwt",
		"token_url":          server.URL + "/token",
		"credential_source":  map[string]string{"file": subject},
	})
	require.NoError(t, err)
	credentialsFile := filepath.Join(dir, "credentials.json")
	require.NoError(t, os.WriteFile(credentialsFile, credentials, 0o600))
	config := &GCSBackendConfig{
		Bucket: "state",
		GCP: &gcpcfg.Configuration{
			Project: new("acme-prod"), Region: new("us-central1"),
			CredentialsFile: new(credentialsFile),
			Endpoints:       new(map[string]string{"storage": server.URL + "/"}),
		},
		Bootstrap: &GCSBootstrapConfig{Location: new("US")},
	}
	_, err = newGCSBackend(config, "", "stack", encrypters.Noop{})
	require.ErrorContains(t, err, "factory is required")
	assert.Empty(t, requests)
	backend, err := newGCSBackend(config, "factory", "stack", encrypters.Noop{})
	require.NoError(t, err)
	_, err = backend.Current()
	require.ErrorIs(t, err, sdkstate.ErrNoCurrent)
	_, err = newGCSBackend(config, "factory", "stack", encrypters.Noop{})
	require.NoError(t, err)
	assert.Equal(t, []string{"GET /b/state", "POST /b",
		"GET /b/state/o/factory/stack/current", "GET /b/state"}, requests)
}
