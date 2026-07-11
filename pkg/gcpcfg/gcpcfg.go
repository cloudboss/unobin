// Package gcpcfg holds the Google Cloud connection settings shared by
// state backends, encrypters, and Go libraries. Load resolves a Configuration
// into credentials and client settings.
package gcpcfg

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/cloudboss/unobin/pkg/defaults"
	"github.com/cloudboss/unobin/pkg/ptr"
)

const defaultRequestTimeout = "120s"

// Configuration selects how a component reaches Google Cloud. Credentials
// come from Application Default Credentials, metadata credentials, or a
// credentials file path; inline tokens and private keys do not belong here.
type Configuration struct {
	Project                            *string
	BillingProject                     *string
	Region                             *string
	Zone                               *string
	CredentialsFile                    *string
	ImpersonateServiceAccount          *string
	ImpersonateServiceAccountDelegates *[]string
	Scopes                             *[]string
	Endpoints                          *map[string]string
	UserProjectOverride                *bool
	RequestReason                      *string
	RequestTimeout                     *string
	UniverseDomain                     *string
	PreferRegionalEndpoints            *bool
	PreferGlobalEndpoints              *bool
}

// DefaultScopes returns the OAuth scopes used when a config does not set
// scopes explicitly.
func DefaultScopes() []string {
	return []string{
		"https://www.googleapis.com/auth/cloud-platform",
		"https://www.googleapis.com/auth/userinfo.email",
	}
}

// Defaults declares schema defaults for source analysis.
func (c Configuration) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.NullableValue(c.Scopes, []string{
			"https://www.googleapis.com/auth/cloud-platform",
			"https://www.googleapis.com/auth/userinfo.email",
		}),
		defaults.NullableValue(c.RequestTimeout, "120s"),
	}
}

// ScopeValues returns configured scopes, or the default scopes when absent.
func (c *Configuration) ScopeValues() []string {
	if c == nil || c.Scopes == nil {
		return DefaultScopes()
	}
	return slices.Clone(*c.Scopes)
}

// Timeout returns the request timeout duration.
func (c *Configuration) Timeout() (time.Duration, error) {
	value := defaultRequestTimeout
	if c != nil && c.RequestTimeout != nil {
		value = *c.RequestTimeout
	}
	d, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("gcp config: request-timeout: %w", err)
	}
	return d, nil
}

// Endpoint returns the endpoint configured for service.
func (c *Configuration) Endpoint(service string) string {
	if c == nil || c.Endpoints == nil {
		return ""
	}
	return (*c.Endpoints)[service]
}

// StorageEndpoint returns the Google Cloud Storage endpoint override.
func (c *Configuration) StorageEndpoint() string {
	return c.Endpoint("storage")
}

// KMSEndpoint returns the Google Cloud KMS endpoint override.
func (c *Configuration) KMSEndpoint() string {
	return c.Endpoint("kms")
}

// Validate checks static configuration that can be rejected without network I/O.
func (c *Configuration) Validate() error {
	if c == nil {
		return nil
	}
	if ptr.Value(c.PreferRegionalEndpoints) && ptr.Value(c.PreferGlobalEndpoints) {
		return errors.New(
			"gcp config: prefer-regional-endpoints and prefer-global-endpoints conflict")
	}
	if _, err := c.Timeout(); err != nil {
		return err
	}
	return nil
}
