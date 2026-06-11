package awscfg

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/sdk/cfg"
)

// isolateEnv points the SDK's file and env lookups away from the real
// machine so tests see only what they set.
func isolateEnv(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(dir, "config"))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(dir, "credentials"))
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("AWS_REGION", "")
	t.Setenv("AWS_DEFAULT_REGION", "")
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")
	t.Setenv("HTTP_PROXY", "")
	t.Setenv("HTTPS_PROXY", "")
	t.Setenv("NO_PROXY", "")
}

func str(v string) *cfg.String { return &cfg.String{Value: v} }

func TestLoadNilUsesChain(t *testing.T) {
	isolateEnv(t)
	t.Setenv("AWS_REGION", "eu-west-3")
	awsCfg, err := Load(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, "eu-west-3", awsCfg.Region)
}

func TestLoadOverrides(t *testing.T) {
	isolateEnv(t)
	c := &Configuration{
		Region:      str("us-east-2"),
		MaxAttempts: &cfg.Integer{Value: 7},
		RetryMode:   str("adaptive"),
		EndpointURL: str("https://minio.example.test:9000"),
	}
	awsCfg, err := Load(context.Background(), c)
	require.NoError(t, err)
	assert.Equal(t, "us-east-2", awsCfg.Region)
	assert.Equal(t, 7, awsCfg.RetryMaxAttempts)
	assert.Equal(t, aws.RetryModeAdaptive, awsCfg.RetryMode)
	require.NotNil(t, awsCfg.BaseEndpoint)
	assert.Equal(t, "https://minio.example.test:9000", *awsCfg.BaseEndpoint)
}

func TestLoadRegionBeatsEnv(t *testing.T) {
	isolateEnv(t)
	t.Setenv("AWS_REGION", "eu-west-3")
	awsCfg, err := Load(context.Background(), &Configuration{Region: str("us-east-1")})
	require.NoError(t, err)
	assert.Equal(t, "us-east-1", awsCfg.Region)
}

func TestLoadRejectsUnknownRetryMode(t *testing.T) {
	isolateEnv(t)
	_, err := Load(context.Background(), &Configuration{RetryMode: str("fancy")})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "retry-mode must be standard or adaptive")
}

func TestLoadRelaxesChecksumsForCustomEndpoint(t *testing.T) {
	isolateEnv(t)
	tests := []struct {
		name    string
		c       *Configuration
		relaxed bool
	}{
		{name: "no endpoint", c: &Configuration{}, relaxed: false},
		{
			name:    "global endpoint",
			c:       &Configuration{EndpointURL: str("https://r2.example.test")},
			relaxed: true,
		},
		{
			name:    "s3 endpoint",
			c:       &Configuration{Endpoints: &Endpoints{S3: str("https://minio.example.test")}},
			relaxed: true,
		},
		{
			name:    "sts endpoint only",
			c:       &Configuration{Endpoints: &Endpoints{STS: str("https://sts.example.test")}},
			relaxed: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			awsCfg, err := Load(context.Background(), tt.c)
			require.NoError(t, err)
			if tt.relaxed {
				assert.Equal(t,
					aws.RequestChecksumCalculationWhenRequired, awsCfg.RequestChecksumCalculation)
				assert.Equal(t,
					aws.ResponseChecksumValidationWhenRequired, awsCfg.ResponseChecksumValidation)
			} else {
				assert.NotEqual(t,
					aws.RequestChecksumCalculationWhenRequired, awsCfg.RequestChecksumCalculation)
			}
		})
	}
}

func TestLoadRejectsBothAssumeRoleForms(t *testing.T) {
	isolateEnv(t)
	c := &Configuration{
		AssumeRole: &AssumeRole{RoleArn: cfg.String{Value: "arn:aws:iam::1:role/a"}},
		AssumeRoleWithWebIdentity: &AssumeRoleWithWebIdentity{
			RoleArn:              cfg.String{Value: "arn:aws:iam::1:role/b"},
			WebIdentityTokenFile: cfg.String{Value: "/tmp/token"},
		},
	}
	_, err := Load(context.Background(), c)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mutually exclusive")
}

func TestLoadAssumeRoleSetsCredentialsCache(t *testing.T) {
	isolateEnv(t)
	c := &Configuration{
		Region: str("us-east-1"),
		AssumeRole: &AssumeRole{
			RoleArn:         cfg.String{Value: "arn:aws:iam::123456789012:role/state-rw"},
			RoleSessionName: str("unobin"),
			ExternalId:      str("xid"),
			DurationSeconds: &cfg.Integer{Value: 3600},
			PolicyArns: &cfg.List[cfg.String]{
				Value: []cfg.String{{Value: "arn:aws:iam::aws:policy/ReadOnlyAccess"}},
			},
			Tags: &cfg.Map[cfg.String]{
				Value: map[string]cfg.String{"team": {Value: "infra"}},
			},
		},
	}
	awsCfg, err := Load(context.Background(), c)
	require.NoError(t, err)
	_, ok := awsCfg.Credentials.(*aws.CredentialsCache)
	assert.True(t, ok, "expected *aws.CredentialsCache, got %T", awsCfg.Credentials)
}

func TestLoadAssumeRoleRequiresArn(t *testing.T) {
	isolateEnv(t)
	_, err := Load(context.Background(), &Configuration{AssumeRole: &AssumeRole{}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "assume-role: role-arn is required")
}

func TestLoadWebIdentitySetsCredentialsCache(t *testing.T) {
	isolateEnv(t)
	c := &Configuration{
		Region: str("us-east-1"),
		AssumeRoleWithWebIdentity: &AssumeRoleWithWebIdentity{
			RoleArn:              cfg.String{Value: "arn:aws:iam::123456789012:role/irsa"},
			WebIdentityTokenFile: cfg.String{Value: "/var/run/token"},
		},
	}
	awsCfg, err := Load(context.Background(), c)
	require.NoError(t, err)
	_, ok := awsCfg.Credentials.(*aws.CredentialsCache)
	assert.True(t, ok, "expected *aws.CredentialsCache, got %T", awsCfg.Credentials)
}

func TestLoadWebIdentityRequiredFields(t *testing.T) {
	isolateEnv(t)
	tests := []struct {
		name string
		wi   *AssumeRoleWithWebIdentity
		want string
	}{
		{
			name: "missing role-arn",
			wi:   &AssumeRoleWithWebIdentity{WebIdentityTokenFile: cfg.String{Value: "/t"}},
			want: "role-arn is required",
		},
		{
			name: "missing token file",
			wi:   &AssumeRoleWithWebIdentity{RoleArn: cfg.String{Value: "arn:x"}},
			want: "web-identity-token-file is required",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(context.Background(),
				&Configuration{AssumeRoleWithWebIdentity: tt.wi})
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestLoadCustomCABundleMissingFile(t *testing.T) {
	isolateEnv(t)
	c := &Configuration{CustomCABundle: str(filepath.Join(t.TempDir(), "missing.pem"))}
	_, err := Load(context.Background(), c)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "custom-ca-bundle")
}

func TestLoadCustomCABundleValid(t *testing.T) {
	isolateEnv(t)
	path := filepath.Join(t.TempDir(), "ca.pem")
	require.NoError(t, os.WriteFile(path, selfSignedPEM(t), 0o600))
	_, err := Load(context.Background(), &Configuration{CustomCABundle: str(path)})
	require.NoError(t, err)
}

func selfSignedPEM(t *testing.T) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "unobin-test"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func TestProxyConfigOverridesEnv(t *testing.T) {
	isolateEnv(t)
	t.Setenv("HTTP_PROXY", "http://env-proxy.test:3128")
	c := &Configuration{
		HTTPSProxy: str("http://cfg-proxy.test:3128"),
		NoProxy:    str("internal.test"),
	}
	pf := proxyConfig(c).ProxyFunc()

	httpsURL, _ := url.Parse("https://bucket.s3.amazonaws.com/key")
	got, err := pf(httpsURL)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "http://cfg-proxy.test:3128", got.String())

	httpURL, _ := url.Parse("http://example.test/")
	got, err = pf(httpURL)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "http://env-proxy.test:3128", got.String())

	skipURL, _ := url.Parse("https://internal.test/")
	got, err = pf(skipURL)
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestLoadProxyInstallsHTTPClient(t *testing.T) {
	isolateEnv(t)
	c := &Configuration{HTTPSProxy: str("http://cfg-proxy.test:3128")}
	awsCfg, err := Load(context.Background(), c)
	require.NoError(t, err)
	_, ok := awsCfg.HTTPClient.(*http.Client)
	assert.False(t, ok, "expected the SDK's buildable client, got *http.Client")
	assert.NotNil(t, awsCfg.HTTPClient)
}
