// Package encrypters holds the fixed set of state encrypters a
// factory can use. An operator selects one by bare name with
// @key-source in a config's encryption block, and the resolver looks
// the name up here. The Encrypter contract lives in pkg/sdk/encrypt.
package encrypters

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/kms"

	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
	sdkencrypt "github.com/cloudboss/unobin/pkg/sdk/encrypt"
)

// Key source names; Describe reports the same name the registry uses
// so a recorded ref resolves back to its type.
const (
	envKeyName = "env-key"
	kmsName    = "kms"
	noopName   = "noop"
)

// Encrypters returns the state encrypters keyed by the bare name an
// operator selects with @key-source. Names are unique by
// construction: this is one map literal, so a duplicate is a compile
// error.
func Encrypters() map[string]sdkencrypt.EncrypterType {
	return map[string]sdkencrypt.EncrypterType{
		envKeyName: {
			Name:        envKeyName,
			Description: "AES-256-GCM with a base64 key read from an env var.",
			Configuration: &cfg.ConfigurationType{
				Description: "Env-key encrypter configuration.",
				New:         func() any { return &EnvKeyConfig{} },
			},
			New: newEnvKey,
		},
		kmsName: {
			Name:        kmsName,
			Description: "AES-256-GCM with data keys wrapped by AWS KMS.",
			Configuration: &cfg.ConfigurationType{
				Description: "KMS encrypter configuration.",
				New:         func() any { return &KMSConfig{} },
			},
			New: newKMSEncrypter,
		},
		noopName: {
			Name:        noopName,
			Description: "No encryption; state is written as plaintext.",
			New:         newNoop,
		},
	}
}

// EnvKeyConfig is the operator-facing body under
// `encryption: { @key-source: env-key ... }`.
type EnvKeyConfig struct {
	EnvVar cfg.String
}

func newEnvKey(config any, _ map[string]any) (sdkencrypt.Encrypter, error) {
	c, ok := config.(*EnvKeyConfig)
	if !ok {
		return nil, fmt.Errorf("env-key encrypter: missing or wrong configuration (got %T)", config)
	}
	return NewEnvKey(c.EnvVar.Value)
}

// KMSConfig is the operator-facing body under
// `encryption: { @key-source: kms ... }`. The aws object holds the
// shared AWS connection settings from pkg/awscfg.
type KMSConfig struct {
	KeyID cfg.String
	AWS   *awscfg.Configuration
}

func newKMSEncrypter(config any, body map[string]any) (sdkencrypt.Encrypter, error) {
	c, ok := config.(*KMSConfig)
	if !ok {
		return nil, fmt.Errorf("kms encrypter: missing or wrong configuration (got %T)", config)
	}
	if c.KeyID.Value == "" {
		return nil, fmt.Errorf("kms encrypter: %s is required", sdkencrypt.ConfigKeyID)
	}
	awsCfg, err := awscfg.Load(context.Background(), c.AWS)
	if err != nil {
		return nil, fmt.Errorf("kms encrypter: %w", err)
	}
	client := kms.NewFromConfig(awsCfg, func(o *kms.Options) {
		if ep := c.AWS.KMSEndpoint(); ep != "" {
			o.BaseEndpoint = aws.String(ep)
		}
	})
	return NewKMS(client, c.KeyID.Value, body)
}

// newNoop builds the no-op encrypter, which writes state as
// plaintext. It is the explicit opt-out for unencrypted state,
// selected as `noop` in a config's encryption block.
func newNoop(_ any, _ map[string]any) (sdkencrypt.Encrypter, error) {
	return Noop{}, nil
}
