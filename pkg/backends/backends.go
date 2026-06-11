// Package backends holds the fixed set of state backends a factory
// can use. An operator selects one by bare name with @backend in a
// config's state: block, and the resolver looks the name up here.
// The encrypter set lives in pkg/encrypters the same way.
package backends

import (
	"context"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/localstate"
	"github.com/cloudboss/unobin/pkg/s3state"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
	sdkencrypt "github.com/cloudboss/unobin/pkg/sdk/encrypt"
	sdkstate "github.com/cloudboss/unobin/pkg/sdk/state"
)

// Backends returns the state backends keyed by the bare name an operator
// selects with @backend. Names are unique by construction: this is one map
// literal, so a duplicate is a compile error.
func Backends() map[string]sdkstate.BackendType {
	return map[string]sdkstate.BackendType{
		"local": {
			Name:        "local",
			Description: "Local filesystem state backend.",
			Configuration: &cfg.ConfigurationType{
				Description: "Local state backend configuration.",
				New:         func() any { return &LocalBackendConfig{} },
			},
			New: newLocalBackend,
		},
		"s3": {
			Name:        "s3",
			Description: "S3 state backend with conditional-write locking.",
			Configuration: &cfg.ConfigurationType{
				Description: "S3 state backend configuration.",
				New:         func() any { return &S3BackendConfig{} },
			},
			New: newS3Backend,
		},
	}
}

// LocalBackendConfig is the operator-facing body under
// `state: { @backend: local ... }`.
type LocalBackendConfig struct {
	Path cfg.String
}

func newLocalBackend(
	config any,
	factory, stack string,
	enc sdkencrypt.Encrypter,
) (sdkstate.Backend, error) {
	c, ok := config.(*LocalBackendConfig)
	if !ok {
		return nil, fmt.Errorf("local backend: missing or wrong configuration (got %T)", config)
	}
	return localstate.NewLocalStore(c.Path.Value, factory, stack, enc)
}

// S3BackendConfig is the operator-facing body under
// `state: { @backend: s3 ... }`. The aws object holds the shared AWS
// connection settings from pkg/awscfg; bucket, prefix, kms-key-id,
// and use-path-style are the backend's own.
type S3BackendConfig struct {
	Bucket       cfg.String
	Prefix       *cfg.String
	KMSKeyID     *cfg.String
	UsePathStyle *cfg.Boolean
	AWS          *awscfg.Configuration
}

func newS3Backend(
	config any,
	factory, stack string,
	enc sdkencrypt.Encrypter,
) (sdkstate.Backend, error) {
	c, ok := config.(*S3BackendConfig)
	if !ok {
		return nil, fmt.Errorf("s3 backend: missing or wrong configuration (got %T)", config)
	}
	if c.Bucket.Value == "" {
		return nil, errors.New("s3 backend: bucket is required")
	}
	awsCfg, err := awscfg.Load(context.Background(), c.AWS)
	if err != nil {
		return nil, fmt.Errorf("s3 backend: %w", err)
	}
	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if ep := c.AWS.S3Endpoint(); ep != "" {
			o.BaseEndpoint = aws.String(ep)
		}
		if c.UsePathStyle != nil {
			o.UsePathStyle = c.UsePathStyle.Value
		}
	})
	return s3state.NewS3Store(client, c.Bucket.Value, optString(c.Prefix),
		optString(c.KMSKeyID), factory, stack, enc)
}

func optString(p *cfg.String) string {
	if p == nil {
		return ""
	}
	return p.Value
}
