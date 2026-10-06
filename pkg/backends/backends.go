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
	localbackend "github.com/cloudboss/unobin/pkg/backends/local"
	"github.com/cloudboss/unobin/pkg/gcpcfg"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
	sdkencrypt "github.com/cloudboss/unobin/pkg/sdk/encrypt"
	sdkstate "github.com/cloudboss/unobin/pkg/sdk/state"
	gcsstore "github.com/cloudboss/unobin/pkg/state/gcs"
	s3store "github.com/cloudboss/unobin/pkg/state/s3"
)

// Backend names, the registry keys an operator selects in stack state.
const (
	LocalName = localbackend.Name
	S3Name    = "s3"
	GCSName   = "gcs"
)

// Backends returns the state backends keyed by the bare name an operator
// selects in stack state. Names are unique by construction: this is one
// map literal, so a duplicate is a compile error.
func Backends() map[string]sdkstate.BackendType {
	return map[string]sdkstate.BackendType{
		LocalName: localbackend.Type(),
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
type LocalBackendConfig = localbackend.Config

// S3BackendConfig is the operator-facing body under `state: s3 { ... }`.
// The aws object holds the shared AWS connection settings from
// pkg/awscfg; bucket, prefix, kms-key-id, use-path-style, and bootstrap are the
// backend's own.
type S3BackendConfig struct {
	Bucket       string
	Prefix       *string
	KMSKeyID     *string
	UsePathStyle *bool
	AWS          *awscfg.Configuration
	Bootstrap    *S3BootstrapConfig
}

func (c *S3BackendConfig) Validate() error {
	if c.Bucket == "" {
		return errors.New("s3 backend: bucket is required")
	}
	return c.Bootstrap.validate()
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
	if err := c.Validate(); err != nil {
		return nil, err
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
	store, err := s3store.NewStore(client, c.Bucket, optString(c.Prefix),
		optString(c.KMSKeyID), factory, stack, enc)
	if err != nil {
		return nil, err
	}
	if err := bootstrapS3Bucket(
		context.Background(), client, c.Bucket, c.Bootstrap, s3VersioningPropagationDelay,
	); err != nil {
		return nil, err
	}
	return store, nil
}

// GCSBackendConfig is the operator-facing body under `state: gcs { ... }`.
type GCSBackendConfig struct {
	Bucket     string
	Prefix     *string
	KMSKeyName *string
	GCP        *gcpcfg.Configuration
	Bootstrap  *GCSBootstrapConfig
}

func (c *GCSBackendConfig) Validate() error {
	if c.Bucket == "" {
		return errors.New("gcs backend: bucket is required")
	}
	if c.GCP != nil {
		if err := c.GCP.Validate(); err != nil {
			return fmt.Errorf("gcs backend: %w", err)
		}
	}
	return c.Bootstrap.validate()
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
	if err := c.Validate(); err != nil {
		return nil, err
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
	store, err := gcsstore.NewStore(client, c.Bucket, optString(c.Prefix),
		optString(c.KMSKeyName), factory, stack, enc)
	if err != nil {
		return nil, err
	}
	var project string
	if c.GCP != nil {
		project = optString(c.GCP.Project)
	}
	if err := bootstrapGCSBucket(
		context.Background(), service, c.Bucket, project, c.Bootstrap,
	); err != nil {
		return nil, err
	}
	return store, nil
}

func optString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
