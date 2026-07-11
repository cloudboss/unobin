package gcpcfg

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/api/transport"
)

func TestLoadNilUsesAmbientConfiguration(t *testing.T) {
	isolateGoogleEnv(t)
	credentialsFile := newTestCredentialsFile(t, "authorized_user")
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", *credentialsFile)
	t.Setenv("CLOUDSDK_CORE_PROJECT", "ambient-project")
	t.Setenv("CLOUDSDK_COMPUTE_REGION", "us-central1")
	t.Setenv("CLOUDSDK_COMPUTE_ZONE", "us-central1-a")
	t.Setenv("GOOGLE_BILLING_PROJECT", "ambient-billing-project")
	t.Setenv("USER_PROJECT_OVERRIDE", "true")
	t.Setenv("CLOUDSDK_CORE_REQUEST_REASON", "ambient request")

	loaded, err := Load(context.Background(), nil)

	require.NoError(t, err)
	assert.Equal(t, "ambient-project", loaded.Project)
	assert.Equal(t, "ambient-billing-project", loaded.BillingProject)
	assert.Equal(t, "us-central1", loaded.Region)
	assert.Equal(t, "us-central1-a", loaded.Zone)
	assert.Equal(t, DefaultScopes(), loaded.Scopes)
	assert.True(t, loaded.UserProjectOverride)
	assert.Equal(t, "ambient request", loaded.RequestReason)
	assert.Equal(t, 120*time.Second, loaded.RequestTimeout)
	assert.Equal(t, "googleapis.com", loaded.UniverseDomain)
	require.NotNil(t, loaded.Credentials)
	require.NotNil(t, loaded.TokenSource)
}

func TestLoadNilUsesGcloudApplicationDefaultCredentials(t *testing.T) {
	isolateGoogleEnv(t)
	source := newTestCredentialsFile(t, "authorized_user")
	body, err := os.ReadFile(*source)
	require.NoError(t, err)
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	credentialsDir := filepath.Join(home, ".config", "gcloud")
	require.NoError(t, os.MkdirAll(credentialsDir, 0o700))
	require.NoError(t, os.WriteFile(
		filepath.Join(credentialsDir, "application_default_credentials.json"), body, 0o600))

	loaded, err := Load(context.Background(), nil)

	require.NoError(t, err)
	assert.Equal(t, "credentials-project", loaded.Project)
	require.NotNil(t, loaded.Credentials)
}

func TestLoadExplicitValuesOverrideEnvironment(t *testing.T) {
	isolateGoogleEnv(t)
	credentialsFile := newTestCredentialsFile(t, "authorized_user")
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", *credentialsFile)
	t.Setenv("GOOGLE_PROJECT", "ambient-project")
	t.Setenv("GOOGLE_BILLING_PROJECT", "ambient-billing-project")
	t.Setenv("GOOGLE_REGION", "ambient-region")
	t.Setenv("GOOGLE_ZONE", "ambient-zone")
	t.Setenv("USER_PROJECT_OVERRIDE", "true")
	t.Setenv("CLOUDSDK_CORE_REQUEST_REASON", "ambient request")
	scopes := []string{"scope-a", "scope-b"}
	endpoints := map[string]string{"storage": "https://storage.example.test/"}
	config := &Configuration{
		Project:                 new("configured-project"),
		BillingProject:          new("configured-billing-project"),
		Region:                  new("configured-region"),
		Zone:                    new("configured-zone"),
		CredentialsFile:         credentialsFile,
		Scopes:                  &scopes,
		Endpoints:               &endpoints,
		UserProjectOverride:     new(false),
		RequestReason:           new("configured request"),
		RequestTimeout:          new("30s"),
		UniverseDomain:          new("googleapis.com"),
		PreferRegionalEndpoints: new(true),
	}

	loaded, err := Load(context.Background(), config)

	require.NoError(t, err)
	assert.Equal(t, "configured-project", loaded.Project)
	assert.Equal(t, "configured-billing-project", loaded.BillingProject)
	assert.Equal(t, "configured-region", loaded.Region)
	assert.Equal(t, "configured-zone", loaded.Zone)
	assert.Equal(t, scopes, loaded.Scopes)
	assert.Equal(t, endpoints, loaded.Endpoints)
	assert.False(t, loaded.UserProjectOverride)
	assert.Equal(t, "configured request", loaded.RequestReason)
	assert.Equal(t, 30*time.Second, loaded.RequestTimeout)
	assert.Equal(t, "googleapis.com", loaded.UniverseDomain)
	assert.True(t, loaded.PreferRegionalEndpoints)
	assert.False(t, loaded.PreferGlobalEndpoints)
}

func TestLoadCredentialsExposeResolvedProperties(t *testing.T) {
	isolateGoogleEnv(t)
	config := &Configuration{
		Project:         new("configured-project"),
		BillingProject:  new("configured-billing-project"),
		CredentialsFile: newTestCredentialsFile(t, "authorized_user"),
		UniverseDomain:  new("googleapis.com"),
	}

	loaded, err := Load(context.Background(), config)
	require.NoError(t, err)
	project, err := loaded.Credentials.ProjectID(context.Background())
	require.NoError(t, err)
	billingProject, err := loaded.Credentials.QuotaProjectID(context.Background())
	require.NoError(t, err)
	universeDomain, err := loaded.Credentials.UniverseDomain(context.Background())
	require.NoError(t, err)

	assert.Equal(t, loaded.Project, project)
	assert.Equal(t, loaded.BillingProject, billingProject)
	assert.Equal(t, loaded.UniverseDomain, universeDomain)
}

func TestLoadDoesNotAliasConfigurationCollections(t *testing.T) {
	isolateGoogleEnv(t)
	scopes := []string{"scope-a", "scope-b"}
	endpoints := map[string]string{"storage": "https://storage.example.test/"}
	config := &Configuration{
		CredentialsFile: newTestCredentialsFile(t, "authorized_user"),
		Scopes:          &scopes,
		Endpoints:       &endpoints,
	}

	loaded, err := Load(context.Background(), config)
	require.NoError(t, err)
	loaded.Scopes[0] = "changed"
	loaded.Endpoints["storage"] = "https://changed.example.test/"

	assert.Equal(t, []string{"scope-a", "scope-b"}, scopes)
	assert.Equal(t,
		map[string]string{"storage": "https://storage.example.test/"}, endpoints)
}

func TestLoadUsesGoogleProjectEnvironmentPrecedence(t *testing.T) {
	tests := []struct {
		name     string
		env      map[string]string
		expected string
	}{
		{
			name: "google project",
			env: map[string]string{
				"GOOGLE_PROJECT":        "google-project",
				"GOOGLE_CLOUD_PROJECT":  "google-cloud-project",
				"GCLOUD_PROJECT":        "gcloud-project",
				"CLOUDSDK_CORE_PROJECT": "cloudsdk-project",
			},
			expected: "google-project",
		},
		{
			name: "google cloud project",
			env: map[string]string{
				"GOOGLE_CLOUD_PROJECT":  "google-cloud-project",
				"GCLOUD_PROJECT":        "gcloud-project",
				"CLOUDSDK_CORE_PROJECT": "cloudsdk-project",
			},
			expected: "google-cloud-project",
		},
		{
			name: "gcloud project",
			env: map[string]string{
				"GCLOUD_PROJECT":        "gcloud-project",
				"CLOUDSDK_CORE_PROJECT": "cloudsdk-project",
			},
			expected: "gcloud-project",
		},
		{
			name:     "cloudsdk project",
			env:      map[string]string{"CLOUDSDK_CORE_PROJECT": "cloudsdk-project"},
			expected: "cloudsdk-project",
		},
		{name: "credentials project", expected: "credentials-project"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolateGoogleEnv(t)
			config := &Configuration{
				CredentialsFile: newTestCredentialsFile(t, "authorized_user"),
			}
			for name, value := range tt.env {
				t.Setenv(name, value)
			}

			loaded, err := Load(context.Background(), config)

			require.NoError(t, err)
			assert.Equal(t, tt.expected, loaded.Project)
		})
	}
}

func TestLoadUsesRegionAndZoneEnvironmentPrecedence(t *testing.T) {
	isolateGoogleEnv(t)
	t.Setenv("GOOGLE_REGION", "google-region")
	t.Setenv("GCLOUD_REGION", "gcloud-region")
	t.Setenv("CLOUDSDK_COMPUTE_REGION", "cloudsdk-region")
	t.Setenv("GOOGLE_ZONE", "google-zone")
	t.Setenv("GCLOUD_ZONE", "gcloud-zone")
	t.Setenv("CLOUDSDK_COMPUTE_ZONE", "cloudsdk-zone")
	config := &Configuration{
		CredentialsFile: newTestCredentialsFile(t, "authorized_user"),
	}

	loaded, err := Load(context.Background(), config)

	require.NoError(t, err)
	assert.Equal(t, "google-region", loaded.Region)
	assert.Equal(t, "google-zone", loaded.Zone)
}

func TestLoadNormalizesRegionSelfLink(t *testing.T) {
	isolateGoogleEnv(t)
	config := &Configuration{
		CredentialsFile: newTestCredentialsFile(t, "authorized_user"),
		Region: new(
			"https://www.googleapis.com/compute/v1/projects/test-project/regions/us-central1"),
	}

	loaded, err := Load(context.Background(), config)

	require.NoError(t, err)
	assert.Equal(t, "us-central1", loaded.Region)
}

func TestLoadUsesUniverseDomainEnvironment(t *testing.T) {
	isolateGoogleEnv(t)
	t.Setenv("GOOGLE_CLOUD_UNIVERSE_DOMAIN", "example.test")
	config := &Configuration{
		CredentialsFile: newTestCredentialsFileWithUniverse(t, "example.test"),
	}

	loaded, err := Load(context.Background(), config)

	require.NoError(t, err)
	assert.Equal(t, "example.test", loaded.UniverseDomain)
}

func TestLoadRejectsUniverseDomainMismatch(t *testing.T) {
	isolateGoogleEnv(t)
	config := &Configuration{
		CredentialsFile: newTestCredentialsFile(t, "authorized_user"),
		UniverseDomain:  new("example.test"),
	}

	_, err := Load(context.Background(), config)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "universe-domain")
	assert.Contains(t, err.Error(), "does not match")
}

func TestLoadExplicitCredentialsFileBeatsADC(t *testing.T) {
	isolateGoogleEnv(t)
	ambientFile := newTestCredentialsFile(t, "authorized_user", "ambient-project")
	explicitFile := newTestCredentialsFile(t, "authorized_user", "explicit-project")
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", *ambientFile)

	loaded, err := Load(context.Background(), &Configuration{
		CredentialsFile: explicitFile,
	})

	require.NoError(t, err)
	assert.Equal(t, "explicit-project", loaded.Project)
}

func TestLoadReturnsWorkingTokenSource(t *testing.T) {
	isolateGoogleEnv(t)
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(
			`{"access_token":"test-access-token","expires_in":3600,"token_type":"Bearer"}`)); err != nil {
			t.Errorf("write token response: %v", err)
		}
	}))
	defer tokenServer.Close()
	config := &Configuration{
		Project:         new("test-project"),
		CredentialsFile: newExternalAccountCredentialsFile(t, tokenServer.URL),
	}

	loaded, err := Load(context.Background(), config)
	require.NoError(t, err)
	token, err := loaded.TokenSource.Token()

	require.NoError(t, err)
	assert.Equal(t, "test-access-token", token.AccessToken)
}

func TestLoadedClientOptionsUseResolvedSession(t *testing.T) {
	isolateGoogleEnv(t)
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(
			`{"access_token":"test-access-token","expires_in":3600,"token_type":"Bearer"}`)); err != nil {
			t.Errorf("write token response: %v", err)
		}
	}))
	defer tokenServer.Close()
	headers := make(chan http.Header, 1)
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers <- r.Header.Clone()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer apiServer.Close()
	endpoints := map[string]string{"storage": apiServer.URL + "/"}
	config := &Configuration{
		Project:         new("test-project"),
		BillingProject:  new("billing-project"),
		CredentialsFile: newExternalAccountCredentialsFile(t, tokenServer.URL),
		Endpoints:       &endpoints,
		RequestReason:   new("test request"),
	}
	loaded, err := Load(context.Background(), config)
	require.NoError(t, err)

	client, endpoint, err := transport.NewHTTPClient(
		context.Background(), loaded.ClientOptions("storage")...)
	require.NoError(t, err)
	request, err := http.NewRequestWithContext(
		context.Background(), http.MethodGet, apiServer.URL, nil)
	require.NoError(t, err)
	response, err := client.Do(request)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	requestHeaders := <-headers

	assert.Equal(t, apiServer.URL+"/", endpoint)
	assert.Equal(t, "Bearer test-access-token", requestHeaders.Get("Authorization"))
	assert.Equal(t, "billing-project", requestHeaders.Get("X-Goog-User-Project"))
	assert.Equal(t, "test request", requestHeaders.Get("X-Goog-Request-Reason"))
}

func TestLoadImpersonationRetainsBaseProject(t *testing.T) {
	isolateGoogleEnv(t)
	config := &Configuration{
		CredentialsFile:           newTestCredentialsFile(t, "authorized_user"),
		ImpersonateServiceAccount: new("target@example.iam.gserviceaccount.com"),
	}

	loaded, err := Load(context.Background(), config)

	require.NoError(t, err)
	assert.Equal(t, "credentials-project", loaded.Project)
	require.NotNil(t, loaded.Credentials)
	require.NotNil(t, loaded.TokenSource)
}

func TestLoadRejectsStaticConfigurationBeforeCredentialDetection(t *testing.T) {
	isolateGoogleEnv(t)

	_, err := Load(context.Background(), &Configuration{RequestTimeout: new("bad")})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "request-timeout")
}

func TestLoadRejectsUnsupportedCredentialsFileType(t *testing.T) {
	isolateGoogleEnv(t)
	config := &Configuration{CredentialsFile: newTestCredentialsFile(t, "unknown")}

	_, err := Load(context.Background(), config)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "credentials-file")
	assert.Contains(t, err.Error(), "unsupported type")
}

func TestLoadRejectsInvalidUserProjectOverrideEnvironment(t *testing.T) {
	isolateGoogleEnv(t)
	t.Setenv("USER_PROJECT_OVERRIDE", "sometimes")
	config := &Configuration{
		CredentialsFile: newTestCredentialsFile(t, "authorized_user"),
	}

	_, err := Load(context.Background(), config)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "user-project-override")
}

func TestClientOptionsRejectsBadTimeoutOrConflictsBeforeNetwork(t *testing.T) {
	badTimeout := &Configuration{RequestTimeout: new("bad")}
	_, err := badTimeout.ClientOptions("storage")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "request-timeout")

	conflict := &Configuration{
		PreferRegionalEndpoints: new(true),
		PreferGlobalEndpoints:   new(true),
	}
	_, err = conflict.ClientOptions("storage")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "prefer-regional-endpoints")
}

func TestClientOptionsAcceptsStaticConfig(t *testing.T) {
	config := &Configuration{
		CredentialsFile:                    newTestCredentialsFile(t, "authorized_user"),
		BillingProject:                     new("billing-project"),
		RequestReason:                      new("test run"),
		UniverseDomain:                     new("googleapis.com"),
		ImpersonateServiceAccount:          new("svc@example.iam.gserviceaccount.com"),
		ImpersonateServiceAccountDelegates: &[]string{"delegate@example.iam.gserviceaccount.com"},
		Endpoints:                          &map[string]string{"storage": "https://storage.test/"},
	}

	opts, err := config.ClientOptions("storage")
	require.NoError(t, err)
	assert.NotEmpty(t, opts)
}

func TestClientOptionsRejectsUnsupportedCredentialsFileType(t *testing.T) {
	config := &Configuration{CredentialsFile: newTestCredentialsFile(t, "unknown")}
	_, err := config.ClientOptions("storage")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "credentials-file")
	assert.Contains(t, err.Error(), "unsupported type")
}

func isolateGoogleEnv(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLOUDSDK_CONFIG", filepath.Join(home, "gcloud"))
	for _, name := range []string{
		"GOOGLE_APPLICATION_CREDENTIALS",
		"GOOGLE_PROJECT",
		"GOOGLE_CLOUD_PROJECT",
		"GCLOUD_PROJECT",
		"CLOUDSDK_CORE_PROJECT",
		"GOOGLE_BILLING_PROJECT",
		"GOOGLE_CLOUD_QUOTA_PROJECT",
		"GOOGLE_REGION",
		"GCLOUD_REGION",
		"CLOUDSDK_COMPUTE_REGION",
		"GOOGLE_ZONE",
		"GCLOUD_ZONE",
		"CLOUDSDK_COMPUTE_ZONE",
		"GOOGLE_IMPERSONATE_SERVICE_ACCOUNT",
		"USER_PROJECT_OVERRIDE",
		"CLOUDSDK_CORE_REQUEST_REASON",
		"GOOGLE_CLOUD_UNIVERSE_DOMAIN",
	} {
		t.Setenv(name, "")
	}
}

func newTestCredentialsFile(t *testing.T, typ string, projects ...string) *string {
	t.Helper()
	project := "credentials-project"
	if len(projects) > 0 {
		project = projects[0]
	}
	body, err := json.Marshal(map[string]string{
		"type":          typ,
		"client_id":     "client-id",
		"client_secret": "client-secret",
		"refresh_token": "refresh-token",
		"project_id":    project,
	})
	require.NoError(t, err)
	return writeCredentialsFile(t, body)
}

func newTestCredentialsFileWithUniverse(t *testing.T, universeDomain string) *string {
	t.Helper()
	body, err := json.Marshal(map[string]string{
		"type":            "authorized_user",
		"client_id":       "client-id",
		"client_secret":   "client-secret",
		"refresh_token":   "refresh-token",
		"project_id":      "credentials-project",
		"universe_domain": universeDomain,
	})
	require.NoError(t, err)
	return writeCredentialsFile(t, body)
}

func newExternalAccountCredentialsFile(t *testing.T, tokenURL string) *string {
	t.Helper()
	subjectPath := filepath.Join(t.TempDir(), "subject-token")
	require.NoError(t, os.WriteFile(subjectPath, []byte("subject-token"), 0o600))
	body, err := json.Marshal(map[string]any{
		"type":               "external_account",
		"audience":           "//iam.googleapis.com/projects/1/locations/global/pools/p/providers/p",
		"subject_token_type": "urn:ietf:params:oauth:token-type:jwt",
		"token_url":          tokenURL,
		"credential_source":  map[string]string{"file": subjectPath},
	})
	require.NoError(t, err)
	return writeCredentialsFile(t, body)
}

func writeCredentialsFile(t *testing.T, body []byte) *string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "credentials.json")
	require.NoError(t, os.WriteFile(path, body, 0o600))
	return &path
}
