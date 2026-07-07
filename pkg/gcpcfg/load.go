package gcpcfg

import "google.golang.org/api/option"

// ClientOptions returns Google API client options for service.
func (c *Configuration) ClientOptions(service string) ([]option.ClientOption, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	opts := []option.ClientOption{option.WithScopes(c.ScopeValues()...)}
	if c == nil {
		return opts, nil
	}
	if v := stringValue(c.CredentialsFile); v != "" {
		opts = append(opts, option.WithCredentialsFile(v))
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
	if v := stringValue(c.ImpersonateServiceAccount); v != "" {
		opts = append(opts, option.ImpersonateCredentials(v, delegateValues(c)...))
	}
	if v := c.Endpoint(service); v != "" {
		opts = append(opts, option.WithEndpoint(v))
	}
	return opts, nil
}

func delegateValues(c *Configuration) []string {
	if c == nil || c.ImpersonateServiceAccountDelegates == nil {
		return nil
	}
	return append([]string(nil), (*c.ImpersonateServiceAccountDelegates)...)
}
