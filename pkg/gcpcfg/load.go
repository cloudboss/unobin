package gcpcfg

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"strconv"
	"strings"
	"time"

	"cloud.google.com/go/auth"
	authcredentials "cloud.google.com/go/auth/credentials"
	authimpersonate "cloud.google.com/go/auth/credentials/impersonate"
	"cloud.google.com/go/auth/oauth2adapt"
	"golang.org/x/oauth2"
	apiimpersonate "google.golang.org/api/impersonate"
	"google.golang.org/api/option"

	"github.com/cloudboss/unobin/pkg/ptr"
)

// ResolvedConfig contains resolved Google Cloud client settings and credentials.
type ResolvedConfig struct {
	Project                 string
	BillingProject          string
	Region                  string
	Zone                    string
	Credentials             *auth.Credentials
	TokenSource             oauth2.TokenSource
	Scopes                  []string
	Endpoints               map[string]string
	UserProjectOverride     bool
	RequestReason           string
	RequestTimeout          time.Duration
	UniverseDomain          string
	PreferRegionalEndpoints bool
	PreferGlobalEndpoints   bool
}

// Load builds a ResolvedConfig from c through Application Default Credentials and
// environment defaults. A nil c applies no explicit overrides.
func Load(ctx context.Context, c *Configuration) (ResolvedConfig, error) {
	if err := c.Validate(); err != nil {
		return ResolvedConfig{}, err
	}
	requestTimeout, err := c.Timeout()
	if err != nil {
		return ResolvedConfig{}, err
	}

	loaded := ResolvedConfig{
		Scopes:         c.ScopeValues(),
		RequestTimeout: requestTimeout,
	}
	var impersonateServiceAccount string
	var userProjectOverrideSet bool
	if c != nil {
		loaded.Project = ptr.Value(c.Project)
		loaded.BillingProject = ptr.Value(c.BillingProject)
		loaded.Region = ptr.Value(c.Region)
		loaded.Zone = ptr.Value(c.Zone)
		loaded.RequestReason = ptr.Value(c.RequestReason)
		loaded.UniverseDomain = ptr.Value(c.UniverseDomain)
		loaded.PreferRegionalEndpoints = ptr.Value(c.PreferRegionalEndpoints)
		loaded.PreferGlobalEndpoints = ptr.Value(c.PreferGlobalEndpoints)
		impersonateServiceAccount = ptr.Value(c.ImpersonateServiceAccount)
		if c.Endpoints != nil {
			loaded.Endpoints = maps.Clone(*c.Endpoints)
		}
		if c.UserProjectOverride != nil {
			loaded.UserProjectOverride = *c.UserProjectOverride
			userProjectOverrideSet = true
		}
	}

	loaded.Project = configuredValue(loaded.Project,
		"GOOGLE_PROJECT", "GOOGLE_CLOUD_PROJECT", "GCLOUD_PROJECT", "CLOUDSDK_CORE_PROJECT")
	loaded.BillingProject = configuredValue(loaded.BillingProject, "GOOGLE_BILLING_PROJECT")
	loaded.Region = regionName(configuredValue(loaded.Region,
		"GOOGLE_REGION", "GCLOUD_REGION", "CLOUDSDK_COMPUTE_REGION"))
	loaded.Zone = configuredValue(
		loaded.Zone, "GOOGLE_ZONE", "GCLOUD_ZONE", "CLOUDSDK_COMPUTE_ZONE")
	impersonateServiceAccount = configuredValue(
		impersonateServiceAccount, "GOOGLE_IMPERSONATE_SERVICE_ACCOUNT")
	loaded.RequestReason = configuredValue(
		loaded.RequestReason, "CLOUDSDK_CORE_REQUEST_REASON")
	loaded.UniverseDomain = configuredValue(
		loaded.UniverseDomain, "GOOGLE_CLOUD_UNIVERSE_DOMAIN")
	if !userProjectOverrideSet {
		loaded.UserProjectOverride, err = userProjectOverrideEnvironmentValue()
		if err != nil {
			return ResolvedConfig{}, err
		}
	}

	baseCredentials, err := loadCredentials(c, loaded.Scopes, loaded.UniverseDomain)
	if err != nil {
		return ResolvedConfig{}, err
	}
	if loaded.Project == "" {
		loaded.Project, err = baseCredentials.ProjectID(ctx)
		if err != nil {
			return ResolvedConfig{}, fmt.Errorf("gcp config: project: %w", err)
		}
	}
	if loaded.BillingProject == "" {
		loaded.BillingProject, err = baseCredentials.QuotaProjectID(ctx)
		if err != nil {
			return ResolvedConfig{}, fmt.Errorf("gcp config: billing-project: %w", err)
		}
	}
	credentialsUniverseDomain, err := baseCredentials.UniverseDomain(ctx)
	if err != nil {
		return ResolvedConfig{}, fmt.Errorf("gcp config: universe-domain: %w", err)
	}
	if loaded.UniverseDomain == "" {
		loaded.UniverseDomain = credentialsUniverseDomain
	} else if credentialsUniverseDomain != loaded.UniverseDomain {
		return ResolvedConfig{}, fmt.Errorf(
			"gcp config: universe-domain '%s' does not match credentials universe domain '%s'",
			loaded.UniverseDomain, credentialsUniverseDomain)
	}

	tokenCredentials := baseCredentials
	if impersonateServiceAccount != "" {
		tokenCredentials, err = authimpersonate.NewCredentials(
			&authimpersonate.CredentialsOptions{
				TargetPrincipal: impersonateServiceAccount,
				Scopes:          loaded.Scopes,
				Delegates:       delegateValues(c),
				Credentials:     baseCredentials,
				UniverseDomain:  loaded.UniverseDomain,
			})
		if err != nil {
			return ResolvedConfig{}, fmt.Errorf("gcp config: impersonate-service-account: %w", err)
		}
	}

	loaded.Credentials = auth.NewCredentials(&auth.CredentialsOptions{
		TokenProvider:          tokenCredentials.TokenProvider,
		JSON:                   baseCredentials.JSON(),
		ProjectIDProvider:      credentialProperty(loaded.Project),
		QuotaProjectIDProvider: credentialProperty(loaded.BillingProject),
		UniverseDomainProvider: credentialProperty(loaded.UniverseDomain),
	})
	loaded.TokenSource = oauth2adapt.TokenSourceFromTokenProvider(
		loaded.Credentials.TokenProvider)
	return loaded, nil
}

// ClientOptions returns Google API client options for service.
func (c *ResolvedConfig) ClientOptions(service string) []option.ClientOption {
	opts := []option.ClientOption{
		option.WithAuthCredentials(c.Credentials),
		option.WithScopes(c.Scopes...),
	}
	if c.BillingProject != "" {
		opts = append(opts, option.WithQuotaProject(c.BillingProject))
	}
	if c.RequestReason != "" {
		opts = append(opts, option.WithRequestReason(c.RequestReason))
	}
	if c.UniverseDomain != "" {
		opts = append(opts, option.WithUniverseDomain(c.UniverseDomain))
	}
	if endpoint := c.Endpoints[service]; endpoint != "" {
		opts = append(opts, option.WithEndpoint(endpoint))
	}
	return opts
}

func loadCredentials(
	c *Configuration,
	scopes []string,
	universeDomain string,
) (*auth.Credentials, error) {
	opts := &authcredentials.DetectOptions{
		Scopes:         scopes,
		UniverseDomain: universeDomain,
	}
	if c != nil {
		if path := ptr.Value(c.CredentialsFile); path != "" {
			credentialType, err := credentialsFileType(path)
			if err != nil {
				return nil, err
			}
			credentials, err := authcredentials.NewCredentialsFromFile(
				authcredentials.CredType(credentialType), path, opts)
			if err != nil {
				return nil, fmt.Errorf("gcp config: credentials-file: %w", err)
			}
			return credentials, nil
		}
	}
	credentials, err := authcredentials.DetectDefault(opts)
	if err != nil {
		return nil, fmt.Errorf("gcp config: credentials: %w", err)
	}
	return credentials, nil
}

func configuredValue(configured string, environmentNames ...string) string {
	if configured != "" {
		return configured
	}
	for _, name := range environmentNames {
		if value := os.Getenv(name); value != "" {
			return value
		}
	}
	return ""
}

func credentialProperty(value string) auth.CredentialsPropertyProvider {
	return auth.CredentialsPropertyFunc(func(context.Context) (string, error) {
		return value, nil
	})
}

func regionName(value string) string {
	_, region, found := strings.Cut(value, "/regions/")
	if !found {
		return value
	}
	region = strings.TrimSuffix(region, "/")
	if region == "" || strings.Contains(region, "/") {
		return value
	}
	return region
}

func userProjectOverrideEnvironmentValue() (bool, error) {
	value := os.Getenv("USER_PROJECT_OVERRIDE")
	if value == "" {
		return false, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("gcp config: user-project-override: %w", err)
	}
	return parsed, nil
}

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
	if v := ptr.Value(c.ImpersonateServiceAccount); v != "" {
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
	if v := ptr.Value(c.BillingProject); v != "" {
		opts = append(opts, option.WithQuotaProject(v))
	}
	if v := ptr.Value(c.RequestReason); v != "" {
		opts = append(opts, option.WithRequestReason(v))
	}
	if v := ptr.Value(c.UniverseDomain); v != "" {
		opts = append(opts, option.WithUniverseDomain(v))
	}
	if v := c.Endpoint(service); v != "" {
		opts = append(opts, option.WithEndpoint(v))
	}
	return opts, nil
}

func (c *Configuration) credentialOptions() ([]option.ClientOption, error) {
	var opts []option.ClientOption
	if v := ptr.Value(c.CredentialsFile); v != "" {
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
	if v := ptr.Value(c.BillingProject); v != "" {
		opts = append(opts, option.WithQuotaProject(v))
	}
	if v := ptr.Value(c.UniverseDomain); v != "" {
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
