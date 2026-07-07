package gcpcfg

import (
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/lang"
)

func TestConfigurationFieldNames(t *testing.T) {
	expected := []string{
		"project",
		"billing-project",
		"region",
		"zone",
		"credentials-file",
		"impersonate-service-account",
		"impersonate-service-account-delegates",
		"scopes",
		"endpoints",
		"user-project-override",
		"request-reason",
		"request-timeout",
		"universe-domain",
		"prefer-regional-endpoints",
		"prefer-global-endpoints",
	}

	assert.Equal(t, expected, ubFieldNames[Configuration]())
}

func TestConfigurationHasNoInlineSecretFields(t *testing.T) {
	forbidden := map[string]bool{
		"Credentials":         true,
		"ExternalCredentials": true,
		"AccessToken":         true,
		"PrivateKey":          true,
		"ClientSecret":        true,
		"RefreshToken":        true,
		"IdentityToken":       true,
	}

	for f := range reflect.TypeFor[Configuration]().Fields() {
		assert.False(t, forbidden[f.Name], "secret field %s must not be in gcpcfg", f.Name)
	}
}

func TestDefaultScopesReturnsFreshSlice(t *testing.T) {
	scopes := DefaultScopes()
	require.NotEmpty(t, scopes)
	scopes[0] = "changed"

	assert.Equal(t, "https://www.googleapis.com/auth/cloud-platform", DefaultScopes()[0])
}

func TestScopeValuesUsesDefault(t *testing.T) {
	assert.Equal(t, DefaultScopes(), new(Configuration).ScopeValues())
}

func TestScopeValuesReturnsConfiguredFreshSlice(t *testing.T) {
	config := &Configuration{Scopes: &[]string{"scope-a", "scope-b"}}
	scopes := config.ScopeValues()
	scopes[0] = "changed"

	assert.Equal(t, []string{"scope-a", "scope-b"}, config.ScopeValues())
}

func TestTimeoutUsesDefault(t *testing.T) {
	got, err := new(Configuration).Timeout()
	require.NoError(t, err)
	assert.Equal(t, 120*time.Second, got)
}

func TestTimeoutParsesDuration(t *testing.T) {
	config := &Configuration{RequestTimeout: new("30s")}

	got, err := config.Timeout()
	require.NoError(t, err)
	assert.Equal(t, 30*time.Second, got)
}

func TestTimeoutRejectsInvalidDuration(t *testing.T) {
	config := &Configuration{RequestTimeout: new("not-a-duration")}

	_, err := config.Timeout()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "request-timeout")
}

func TestEndpointHelpers(t *testing.T) {
	var nilConfig *Configuration
	assert.Empty(t, nilConfig.Endpoint("storage"))
	assert.Empty(t, nilConfig.StorageEndpoint())
	assert.Empty(t, nilConfig.KMSEndpoint())

	config := &Configuration{
		Endpoints: &map[string]string{
			"storage": "https://storage.example.test/",
			"kms":     "https://kms.example.test/",
			"other":   "https://other.example.test/",
		},
	}

	assert.Equal(t, "https://storage.example.test/", config.Endpoint("storage"))
	assert.Equal(t, "https://storage.example.test/", config.StorageEndpoint())
	assert.Equal(t, "https://kms.example.test/", config.Endpoint("kms"))
	assert.Equal(t, "https://kms.example.test/", config.KMSEndpoint())
	assert.Empty(t, config.Endpoint("missing"))
}

func TestValidateRejectsConflictingEndpointPreferences(t *testing.T) {
	config := &Configuration{
		PreferRegionalEndpoints: new(true),
		PreferGlobalEndpoints:   new(true),
	}

	err := config.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "prefer-regional-endpoints")
	assert.Contains(t, err.Error(), "prefer-global-endpoints")
}

func ubFieldNames[T any]() []string {
	var names []string
	for f := range reflect.TypeFor[T]().Fields() {
		if tag := f.Tag.Get("ub"); tag != "" {
			names = append(names, tag)
			continue
		}
		names = append(names, lang.PascalToKebab(f.Name))
	}
	return names
}
