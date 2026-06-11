// Package awscfg holds the AWS connection settings shared by every
// component that reaches AWS. The Configuration struct is the
// operator-facing `aws:` object: the s3 state backend nests it beside
// its own fields, and other AWS-backed components compose the same
// struct so the option names and credential behavior stay identical
// everywhere. Load turns a Configuration into an aws.Config through
// the SDK's default credential chain.
package awscfg

import (
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
)

// Configuration selects how a component reaches AWS. Every field is
// optional; an empty or nil Configuration means the SDK's default
// chain alone: env credentials, shared config and credentials files,
// SSO, web identity, container credentials, then IMDS. Static
// credential fields are deliberately absent; credentials enter
// through the chain, a profile, or role assumption.
type Configuration struct {
	Region                    *cfg.String
	Profile                   *cfg.String
	EndpointURL               *cfg.String
	Endpoints                 *Endpoints
	MaxAttempts               *cfg.Integer
	RetryMode                 *cfg.String
	SharedConfigFiles         *cfg.List[cfg.String]
	SharedCredentialsFiles    *cfg.List[cfg.String]
	CustomCABundle            *cfg.String
	HTTPProxy                 *cfg.String
	HTTPSProxy                *cfg.String
	NoProxy                   *cfg.String
	AssumeRole                *AssumeRole
	AssumeRoleWithWebIdentity *AssumeRoleWithWebIdentity
}

// Endpoints overrides the endpoint of one service at a time, for
// S3-compatible object stores and private STS endpoints. A service
// without an entry falls back to endpoint-url, then to the SDK's own
// resolution, including the AWS_ENDPOINT_URL_* env vars.
type Endpoints struct {
	S3  *cfg.String
	STS *cfg.String
}

// AssumeRole assumes an IAM role using the chain's credentials as the
// source identity.
type AssumeRole struct {
	RoleArn           cfg.String
	RoleSessionName   *cfg.String
	ExternalId        *cfg.String
	DurationSeconds   *cfg.Integer
	Policy            *cfg.String
	PolicyArns        *cfg.List[cfg.String]
	SourceIdentity    *cfg.String
	Tags              *cfg.Map[cfg.String]
	TransitiveTagKeys *cfg.List[cfg.String]
}

// AssumeRoleWithWebIdentity assumes an IAM role with an OIDC token
// read from a file. The token is always file-sourced; a literal token
// in static configuration would be expired by definition.
type AssumeRoleWithWebIdentity struct {
	RoleArn              cfg.String
	WebIdentityTokenFile cfg.String
	RoleSessionName      *cfg.String
	DurationSeconds      *cfg.Integer
	Policy               *cfg.String
	PolicyArns           *cfg.List[cfg.String]
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

func stringValue(p *cfg.String) string {
	if p == nil {
		return ""
	}
	return p.Value
}

func listValues(p *cfg.List[cfg.String]) []string {
	if p == nil {
		return nil
	}
	out := make([]string, 0, len(p.Value))
	for _, v := range p.Value {
		out = append(out, v.Value)
	}
	return out
}

func mapValues(p *cfg.Map[cfg.String]) map[string]string {
	if p == nil {
		return nil
	}
	out := make(map[string]string, len(p.Value))
	for k, v := range p.Value {
		out[k] = v.Value
	}
	return out
}
