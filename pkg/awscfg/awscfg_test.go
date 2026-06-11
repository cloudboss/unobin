package awscfg

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/cloudboss/unobin/pkg/lang"
)

// The decoder maps Go fields to UB keys with PascalToKebab and no tag
// override, so every exported field must kebab to exactly the
// operator-facing name. This pins the full option vocabulary.
func TestFieldNamesKebab(t *testing.T) {
	tests := []struct {
		name     string
		typ      reflect.Type
		expected []string
	}{
		{
			name: "Configuration",
			typ:  reflect.TypeFor[Configuration](),
			expected: []string{
				"region", "profile", "endpoint-url", "endpoints",
				"max-attempts", "retry-mode", "shared-config-files",
				"shared-credentials-files", "custom-ca-bundle",
				"http-proxy", "https-proxy", "no-proxy", "assume-role",
				"assume-role-with-web-identity",
			},
		},
		{
			name:     "Endpoints",
			typ:      reflect.TypeFor[Endpoints](),
			expected: []string{"s3", "sts", "kms"},
		},
		{
			name: "AssumeRole",
			typ:  reflect.TypeFor[AssumeRole](),
			expected: []string{
				"role-arn", "role-session-name", "external-id",
				"duration-seconds", "policy", "policy-arns",
				"source-identity", "tags", "transitive-tag-keys",
			},
		},
		{
			name: "AssumeRoleWithWebIdentity",
			typ:  reflect.TypeFor[AssumeRoleWithWebIdentity](),
			expected: []string{
				"role-arn", "web-identity-token-file", "role-session-name",
				"duration-seconds", "policy", "policy-arns",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []string
			for f := range tt.typ.Fields() {
				got = append(got, lang.PascalToKebab(f.Name))
			}
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestEndpointGetters(t *testing.T) {
	global := str("https://global.test")
	s3 := str("https://s3.test")
	sts := str("https://sts.test")
	kms := str("https://kms.test")
	tests := []struct {
		name    string
		c       *Configuration
		wantS3  string
		wantSTS string
		wantKMS string
	}{
		{name: "nil configuration", c: nil},
		{name: "nothing set", c: &Configuration{}},
		{
			name:    "global only",
			c:       &Configuration{EndpointURL: global},
			wantS3:  "https://global.test",
			wantSTS: "https://global.test",
			wantKMS: "https://global.test",
		},
		{
			name:    "s3 override beats global",
			c:       &Configuration{EndpointURL: global, Endpoints: &Endpoints{S3: s3}},
			wantS3:  "https://s3.test",
			wantSTS: "https://global.test",
			wantKMS: "https://global.test",
		},
		{
			name:    "sts override beats global",
			c:       &Configuration{EndpointURL: global, Endpoints: &Endpoints{STS: sts}},
			wantS3:  "https://global.test",
			wantSTS: "https://sts.test",
			wantKMS: "https://global.test",
		},
		{
			name:    "kms override beats global",
			c:       &Configuration{EndpointURL: global, Endpoints: &Endpoints{KMS: kms}},
			wantS3:  "https://global.test",
			wantSTS: "https://global.test",
			wantKMS: "https://kms.test",
		},
		{
			name:    "service overrides without global",
			c:       &Configuration{Endpoints: &Endpoints{S3: s3, STS: sts, KMS: kms}},
			wantS3:  "https://s3.test",
			wantSTS: "https://sts.test",
			wantKMS: "https://kms.test",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantS3, tt.c.S3Endpoint())
			assert.Equal(t, tt.wantSTS, tt.c.STSEndpoint())
			assert.Equal(t, tt.wantKMS, tt.c.KMSEndpoint())
		})
	}
}
