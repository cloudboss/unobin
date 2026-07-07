// Package backends holds the fixed set of state backends a factory
// can use. An operator selects one by bare name in a stack state
// declaration, and the resolver looks the name up here. The encrypter
// set lives in pkg/encrypters the same way.
package backends

import (
	"context"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	gcstorage "google.golang.org/api/storage/v1"

	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/gcpcfg"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
	sdkencrypt "github.com/cloudboss/unobin/pkg/sdk/encrypt"
	sdkstate "github.com/cloudboss/unobin/pkg/sdk/state"
	gcsstore "github.com/cloudboss/unobin/pkg/state/gcs"
	"github.com/cloudboss/unobin/pkg/state/local"
	s3store "github.com/cloudboss/unobin/pkg/state/s3"
)

// Backend names, the registry keys an operator selects in stack state.
const (
	LocalName = "local"
	S3Name    = "s3"
	GCSName   = "gcs"
)

// Backends returns the state backends keyed by the bare name an operator
// selects in stack state. Names are unique by construction: this is one
// map literal, so a duplicate is a compile error.
func Backends() map[string]sdkstate.BackendType {
	return map[string]sdkstate.BackendType{
		LocalName: {
			Name:        LocalName,
			Description: "Local filesystem state backend.",
			Configuration: &cfg.ConfigurationType[any]{
				Description: "Local state backend configuration.",
				New:         func() any { return &LocalBackendConfig{} },
			},
			New: newLocalBackend,
		},
		S3Name: {
			Name:        S3Name,
			Description: "S3 state backend with conditional-write locking.",
			Configuration: &cfg.ConfigurationType[any]{
				Description: "S3 state backend configuration.",
				New:         func() any { return &S3BackendConfig{} },
			},
			New: newS3Backend,
		},
		GCSName: {
			Name:        GCSName,
			Description: "GCS state backend with generation-precondition locking.",
			Configuration: &cfg.ConfigurationType[any]{
				Description: "GCS state backend configuration.",
				New:         func() any { return &GCSBackendConfig{} },
			},
			New: newGCSBackend,
		},
	}
}

// LocalBackendConfig is the operator-facing body under
// `state: local { ... }`.
type LocalBackendConfig struct {
	Path string
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
	if c.Path == "" {
		return nil, errors.New("local backend: path is required")
	}
	return local.NewStore(c.Path, factory, stack, enc)
}

// S3BackendConfig is the operator-facing body under `state: s3 { ... }`.
// The aws object holds the shared AWS connection settings from
// pkg/awscfg; bucket, prefix, kms-key-id, and use-path-style are the
// backend's own.
type S3BackendConfig struct {
	Bucket       string
	Prefix       *string
	KMSKeyID     *string
	UsePathStyle *bool
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
	if c.Bucket == "" {
		return nil, errors.New("s3 backend: bucket is required")
	}
	awsCfg, err := awscfg.Load(context.Background(), c.AWS)
	if err != nil {
		return nil, fmt.Errorf("s3 backend: %w", err)
	}
	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if c.AWS != nil {
			if ep := c.AWS.S3Endpoint(); ep != "" {
				o.BaseEndpoint = aws.String(ep)
			}
		}
		if c.UsePathStyle != nil {
			o.UsePathStyle = *c.UsePathStyle
		}
	})
	return s3store.NewStore(client, c.Bucket, optString(c.Prefix),
		optString(c.KMSKeyID), factory, stack, enc)
}

// GCSBackendConfig is the operator-facing body under `state: gcs { ... }`.
type GCSBackendConfig struct {
	Bucket     string
	Prefix     *string
	KMSKeyName *string
	GCP        *gcpcfg.Configuration
}

func newGCSBackend(
	config any,
	factory, stack string,
	enc sdkencrypt.Encrypter,
) (sdkstate.Backend, error) {
	c, ok := config.(*GCSBackendConfig)
	if !ok {
		return nil, fmt.Errorf("gcs backend: missing or wrong configuration (got %T)", config)
	}
	if c.Bucket == "" {
		return nil, errors.New("gcs backend: bucket is required")
	}
	opts, err := c.GCP.ClientOptions("storage")
	if err != nil {
		return nil, fmt.Errorf("gcs backend: %w", err)
	}
	service, err := gcstorage.NewService(context.Background(), opts...)
	if err != nil {
		return nil, fmt.Errorf("gcs backend: %w", err)
	}
	client := gcsstore.NewClient(service, c.Bucket)
	return gcsstore.NewStore(client, c.Bucket, optString(c.Prefix),
		optString(c.KMSKeyName), factory, stack, enc)
}

func optString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
