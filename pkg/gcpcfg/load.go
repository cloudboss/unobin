package gcpcfg

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	apiimpersonate "google.golang.org/api/impersonate"
	"google.golang.org/api/option"
)

// ClientOptions returns Google API client options for service.
func (c *Configuration) ClientOptions(service string) ([]option.ClientOption, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	opts := []option.ClientOption{option.WithScopes(c.ScopeValues()...)}
	if c == nil {
		return opts, nil
	}
	credentialOpts, err := c.credentialOptions()
	if err != nil {
		return nil, err
	}
	if v := stringValue(c.ImpersonateServiceAccount); v != "" {
		baseOpts := c.impersonationBaseOptions(credentialOpts)
		tokenSource, err := apiimpersonate.CredentialsTokenSource(
			context.Background(),
			apiimpersonate.CredentialsConfig{
				TargetPrincipal: v,
				Scopes:          c.ScopeValues(),
				Delegates:       delegateValues(c),
			},
			baseOpts...,
		)
		if err != nil {
			return nil, fmt.Errorf("gcp config: impersonate-service-account: %w", err)
		}
		opts = append(opts, option.WithTokenSource(tokenSource))
	} else {
		opts = append(opts, credentialOpts...)
	}
	if v := stringValue(c.BillingProject); v != "" {
		opts = append(opts, option.WithQuotaProject(v))
	}
	if v := stringValue(c.RequestReason); v != "" {
		opts = append(opts, option.WithRequestReason(v))
	}
	if v := stringValue(c.UniverseDomain); v != "" {
		opts = append(opts, option.WithUniverseDomain(v))
	}
	if v := c.Endpoint(service); v != "" {
		opts = append(opts, option.WithEndpoint(v))
	}
	return opts, nil
}

func (c *Configuration) credentialOptions() ([]option.ClientOption, error) {
	var opts []option.ClientOption
	if v := stringValue(c.CredentialsFile); v != "" {
		credType, err := credentialsFileType(v)
		if err != nil {
			return nil, err
		}
		opts = append(opts, option.WithAuthCredentialsFile(credType, v))
	}
	return opts, nil
}

func (c *Configuration) impersonationBaseOptions(
	credentialOpts []option.ClientOption,
) []option.ClientOption {
	opts := append([]option.ClientOption(nil), credentialOpts...)
	if v := stringValue(c.BillingProject); v != "" {
		opts = append(opts, option.WithQuotaProject(v))
	}
	if v := stringValue(c.UniverseDomain); v != "" {
		opts = append(opts, option.WithUniverseDomain(v))
	}
	return opts
}

func credentialsFileType(path string) (option.CredentialsType, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("gcp config: credentials-file: %w", err)
	}
	var header struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(body, &header); err != nil {
		return "", fmt.Errorf("gcp config: credentials-file: %w", err)
	}
	switch header.Type {
	case "service_account":
		return option.ServiceAccount, nil
	case "authorized_user":
		return option.AuthorizedUser, nil
	case "external_account":
		return option.ExternalAccount, nil
	case "impersonated_service_account":
		return option.ImpersonatedServiceAccount, nil
	default:
		return "", fmt.Errorf("gcp config: credentials-file: unsupported type %q", header.Type)
	}
}

func delegateValues(c *Configuration) []string {
	if c == nil || c.ImpersonateServiceAccountDelegates == nil {
		return nil
	}
	return append([]string(nil), (*c.ImpersonateServiceAccountDelegates)...)
}
