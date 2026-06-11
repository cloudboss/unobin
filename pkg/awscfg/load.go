package awscfg

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"os"
	"slices"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	sdkconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	ststypes "github.com/aws/aws-sdk-go-v2/service/sts/types"
	"golang.org/x/net/http/httpproxy"
)

// Load builds an aws.Config from c through the SDK's default
// credential chain. A nil c uses the chain with no overrides. When an
// object-store endpoint override is set, request and response
// checksums relax to when-required, since stores outside AWS commonly
// reject the data-integrity headers.
func Load(ctx context.Context, c *Configuration) (aws.Config, error) {
	if c == nil {
		return sdkconfig.LoadDefaultConfig(ctx)
	}
	if c.AssumeRole != nil && c.AssumeRoleWithWebIdentity != nil {
		return aws.Config{}, errors.New(
			"aws: assume-role and assume-role-with-web-identity are mutually exclusive")
	}
	opts, err := loadOptions(c)
	if err != nil {
		return aws.Config{}, err
	}
	awsCfg, err := sdkconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return aws.Config{}, err
	}
	if c.AssumeRole != nil {
		if err := applyAssumeRole(&awsCfg, c); err != nil {
			return aws.Config{}, err
		}
	}
	if c.AssumeRoleWithWebIdentity != nil {
		if err := applyWebIdentity(&awsCfg, c); err != nil {
			return aws.Config{}, err
		}
	}
	return awsCfg, nil
}

func loadOptions(c *Configuration) ([]func(*sdkconfig.LoadOptions) error, error) {
	var opts []func(*sdkconfig.LoadOptions) error
	if v := stringValue(c.Region); v != "" {
		opts = append(opts, sdkconfig.WithRegion(v))
	}
	if v := stringValue(c.Profile); v != "" {
		opts = append(opts, sdkconfig.WithSharedConfigProfile(v))
	}
	if vs := listValues(c.SharedConfigFiles); len(vs) > 0 {
		opts = append(opts, sdkconfig.WithSharedConfigFiles(vs))
	}
	if vs := listValues(c.SharedCredentialsFiles); len(vs) > 0 {
		opts = append(opts, sdkconfig.WithSharedCredentialsFiles(vs))
	}
	if c.MaxAttempts != nil && c.MaxAttempts.Value > 0 {
		opts = append(opts, sdkconfig.WithRetryMaxAttempts(int(c.MaxAttempts.Value)))
	}
	if v := stringValue(c.RetryMode); v != "" {
		mode, err := retryMode(v)
		if err != nil {
			return nil, err
		}
		opts = append(opts, sdkconfig.WithRetryMode(mode))
	}
	if v := stringValue(c.EndpointURL); v != "" {
		opts = append(opts, sdkconfig.WithBaseEndpoint(v))
	}
	if v := stringValue(c.CustomCABundle); v != "" {
		pemBytes, err := os.ReadFile(v)
		if err != nil {
			return nil, fmt.Errorf("aws: custom-ca-bundle: %w", err)
		}
		opts = append(opts, sdkconfig.WithCustomCABundle(bytes.NewReader(pemBytes)))
	}
	if stringValue(c.HTTPProxy) != "" || stringValue(c.HTTPSProxy) != "" ||
		stringValue(c.NoProxy) != "" {
		opts = append(opts, sdkconfig.WithHTTPClient(proxyHTTPClient(c)))
	}
	if c.S3Endpoint() != "" {
		opts = append(opts,
			sdkconfig.WithRequestChecksumCalculation(aws.RequestChecksumCalculationWhenRequired),
			sdkconfig.WithResponseChecksumValidation(aws.ResponseChecksumValidationWhenRequired))
	}
	return opts, nil
}

func retryMode(v string) (aws.RetryMode, error) {
	switch v {
	case "standard":
		return aws.RetryModeStandard, nil
	case "adaptive":
		return aws.RetryModeAdaptive, nil
	}
	return "", fmt.Errorf("aws: retry-mode must be standard or adaptive, got '%s'", v)
}

// proxyConfig starts from the proxy env vars and overrides whichever
// of http-proxy, https-proxy, and no-proxy are set, so a partial
// override keeps the env behavior for the rest.
func proxyConfig(c *Configuration) *httpproxy.Config {
	pc := httpproxy.FromEnvironment()
	if v := stringValue(c.HTTPProxy); v != "" {
		pc.HTTPProxy = v
	}
	if v := stringValue(c.HTTPSProxy); v != "" {
		pc.HTTPSProxy = v
	}
	if v := stringValue(c.NoProxy); v != "" {
		pc.NoProxy = v
	}
	return pc
}

func proxyHTTPClient(c *Configuration) *awshttp.BuildableClient {
	pf := proxyConfig(c).ProxyFunc()
	return awshttp.NewBuildableClient().WithTransportOptions(func(tr *http.Transport) {
		tr.Proxy = func(req *http.Request) (*url.URL, error) {
			return pf(req.URL)
		}
	})
}

func applyAssumeRole(awsCfg *aws.Config, c *Configuration) error {
	ar := c.AssumeRole
	if ar.RoleArn.Value == "" {
		return errors.New("aws: assume-role: role-arn is required")
	}
	provider := stscreds.NewAssumeRoleProvider(stsClient(*awsCfg, c), ar.RoleArn.Value,
		func(o *stscreds.AssumeRoleOptions) {
			if v := stringValue(ar.RoleSessionName); v != "" {
				o.RoleSessionName = v
			}
			if v := stringValue(ar.ExternalId); v != "" {
				o.ExternalID = aws.String(v)
			}
			if ar.DurationSeconds != nil && ar.DurationSeconds.Value > 0 {
				o.Duration = time.Duration(ar.DurationSeconds.Value) * time.Second
			}
			if v := stringValue(ar.Policy); v != "" {
				o.Policy = aws.String(v)
			}
			for _, arn := range listValues(ar.PolicyArns) {
				o.PolicyARNs = append(o.PolicyARNs,
					ststypes.PolicyDescriptorType{Arn: aws.String(arn)})
			}
			if v := stringValue(ar.SourceIdentity); v != "" {
				o.SourceIdentity = aws.String(v)
			}
			tags := mapValues(ar.Tags)
			for _, k := range slices.Sorted(maps.Keys(tags)) {
				o.Tags = append(o.Tags,
					ststypes.Tag{Key: aws.String(k), Value: aws.String(tags[k])})
			}
			o.TransitiveTagKeys = append(
				o.TransitiveTagKeys, listValues(ar.TransitiveTagKeys)...)
		})
	awsCfg.Credentials = aws.NewCredentialsCache(provider)
	return nil
}

func applyWebIdentity(awsCfg *aws.Config, c *Configuration) error {
	wi := c.AssumeRoleWithWebIdentity
	if wi.RoleArn.Value == "" {
		return errors.New("aws: assume-role-with-web-identity: role-arn is required")
	}
	if wi.WebIdentityTokenFile.Value == "" {
		return errors.New(
			"aws: assume-role-with-web-identity: web-identity-token-file is required")
	}
	provider := stscreds.NewWebIdentityRoleProvider(stsClient(*awsCfg, c),
		wi.RoleArn.Value, stscreds.IdentityTokenFile(wi.WebIdentityTokenFile.Value),
		func(o *stscreds.WebIdentityRoleOptions) {
			if v := stringValue(wi.RoleSessionName); v != "" {
				o.RoleSessionName = v
			}
			if wi.DurationSeconds != nil && wi.DurationSeconds.Value > 0 {
				o.Duration = time.Duration(wi.DurationSeconds.Value) * time.Second
			}
			if v := stringValue(wi.Policy); v != "" {
				o.Policy = aws.String(v)
			}
			for _, arn := range listValues(wi.PolicyArns) {
				o.PolicyARNs = append(o.PolicyARNs,
					ststypes.PolicyDescriptorType{Arn: aws.String(arn)})
			}
		})
	awsCfg.Credentials = aws.NewCredentialsCache(provider)
	return nil
}

// stsClient builds the STS client role assumption goes through,
// honoring a per-service or global endpoint override.
func stsClient(awsCfg aws.Config, c *Configuration) *sts.Client {
	return sts.NewFromConfig(awsCfg, func(o *sts.Options) {
		if ep := c.STSEndpoint(); ep != "" {
			o.BaseEndpoint = aws.String(ep)
		}
	})
}
