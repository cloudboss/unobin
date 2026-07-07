package gcpcfg

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

func newTestCredentialsFile(t *testing.T, typ string) *string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "credentials.json")
	body := []byte(`{
		"type": "` + typ + `",
		"client_id": "client-id",
		"client_secret": "client-secret",
		"refresh_token": "refresh-token"
	}`)
	require.NoError(t, os.WriteFile(path, body, 0o600))
	return &path
}
