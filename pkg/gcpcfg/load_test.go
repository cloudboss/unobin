package gcpcfg

import (
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
		CredentialsFile:                    new("/tmp/service-account.json"),
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
