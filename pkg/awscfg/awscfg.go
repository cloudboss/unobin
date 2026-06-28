// Package awscfg holds the AWS connection settings shared by every
// component that reaches AWS. The Configuration struct is the
// operator-facing `aws:` object: the s3 state backend nests it beside
// its own fields, and other AWS-backed components compose the same
// struct so the option names and credential behavior stay identical
// everywhere. Load turns a Configuration into an aws.Config through
// the SDK's default credential chain.
package awscfg

import (
	"maps"
	"slices"
)

// Configuration selects how a component reaches AWS. Every field is
// optional; an empty or nil Configuration means the SDK's default
// chain alone: env credentials, shared config and credentials files,
// SSO, web identity, container credentials, then IMDS. Static
// credential fields are deliberately absent; credentials enter
// through the chain, a profile, or role assumption.
type Configuration struct {
	Region                    *string
	Profile                   *string
	EndpointURL               *string
	Endpoints                 *Endpoints
	MaxAttempts               *int64
	RetryMode                 *string
	SharedConfigFiles         *[]string
	SharedCredentialsFiles    *[]string
	CustomCABundle            *string
	HTTPProxy                 *string
	HTTPSProxy                *string
	NoProxy                   *string
	AssumeRole                *AssumeRole
	AssumeRoleWithWebIdentity *AssumeRoleWithWebIdentity
}

// Endpoints overrides the endpoint of one service at a time, for
// S3-compatible object stores and private STS or KMS endpoints. A
// service without an entry falls back to endpoint-url, then to the
// SDK's own resolution, including the AWS_ENDPOINT_URL_* env vars.
type Endpoints struct {
	S3  *string
	STS *string
	KMS *string
}

// AssumeRole assumes an IAM role using the chain's credentials as the
// source identity.
type AssumeRole struct {
	RoleArn           string
	RoleSessionName   *string
	ExternalId        *string
	DurationSeconds   *int64
	Policy            *string
	PolicyArns        *[]string
	SourceIdentity    *string
	Tags              *map[string]string
	TransitiveTagKeys *[]string
}

// AssumeRoleWithWebIdentity assumes an IAM role with an OIDC token
// read from a file. The token is always file-sourced; a literal token
// in static configuration would be expired by definition.
type AssumeRoleWithWebIdentity struct {
	RoleArn              string
	WebIdentityTokenFile string
	RoleSessionName      *string
	DurationSeconds      *int64
	Policy               *string
	PolicyArns           *[]string
}

// S3Endpoint returns the endpoint override an S3 client should use:
// endpoints.s3 when set, else endpoint-url, else empty.
func (c *Configuration) S3Endpoint() string {
	if c == nil {
		return ""
	}
	if c.Endpoints != nil {
		if v := stringValue(c.Endpoints.S3); v != "" {
			return v
		}
	}
	return stringValue(c.EndpointURL)
}

// STSEndpoint returns the endpoint override an STS client should use:
// endpoints.sts when set, else endpoint-url, else empty.
func (c *Configuration) STSEndpoint() string {
	if c == nil {
		return ""
	}
	if c.Endpoints != nil {
		if v := stringValue(c.Endpoints.STS); v != "" {
			return v
		}
	}
	return stringValue(c.EndpointURL)
}

// KMSEndpoint returns the endpoint override a KMS client should use:
// endpoints.kms when set, else endpoint-url, else empty.
func (c *Configuration) KMSEndpoint() string {
	if c == nil {
		return ""
	}
	if c.Endpoints != nil {
		if v := stringValue(c.Endpoints.KMS); v != "" {
			return v
		}
	}
	return stringValue(c.EndpointURL)
}

func stringValue(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func listValues(p *[]string) []string {
	if p == nil {
		return nil
	}
	return slices.Clone(*p)
}

func mapValues(p *map[string]string) map[string]string {
	if p == nil {
		return nil
	}
	return maps.Clone(*p)
}
