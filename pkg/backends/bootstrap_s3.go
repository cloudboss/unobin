package backends

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

const s3VersioningPropagationDelay = 15 * time.Minute

type S3BootstrapConfig struct {
	BucketNamespace   *string
	Versioning        *S3BucketVersioning
	PublicAccessBlock *S3BucketPublicAccessBlock
	OwnershipControls *S3BucketOwnershipControls
	Tags              *map[string]string
}

type S3BucketVersioning struct {
	Status string
}

type S3BucketPublicAccessBlock struct {
	BlockPublicAcls       *bool
	BlockPublicPolicy     *bool
	IgnorePublicAcls      *bool
	RestrictPublicBuckets *bool
}

type S3BucketOwnershipControls struct {
	ObjectOwnership string
}

func (c *S3BootstrapConfig) enabled() bool {
	return c != nil && *c != (S3BootstrapConfig{})
}

func (c *S3BootstrapConfig) validate() error {
	if !c.enabled() {
		return nil
	}
	if c.BucketNamespace != nil {
		switch *c.BucketNamespace {
		case "global", "account-regional":
		default:
			return errors.New("s3 bootstrap.bucket-namespace must be global or account-regional")
		}
	}
	if c.Versioning != nil {
		switch c.Versioning.Status {
		case "Enabled", "Suspended":
		default:
			return errors.New("s3 bootstrap.versioning.status must be Enabled or Suspended")
		}
	}
	if c.OwnershipControls != nil {
		switch c.OwnershipControls.ObjectOwnership {
		case "BucketOwnerEnforced", "BucketOwnerPreferred", "ObjectWriter":
		default:
			return errors.New("s3 bootstrap.ownership-controls.object-ownership must be " +
				"BucketOwnerEnforced, BucketOwnerPreferred, or ObjectWriter")
		}
	}
	return nil
}

func bootstrapS3Bucket(
	ctx context.Context,
	client *s3.Client,
	bucket string,
	config *S3BootstrapConfig,
	versioningDelay time.Duration,
) error {
	if !config.enabled() {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute+versioningDelay)
	defer cancel()
	head := &s3.HeadBucketInput{Bucket: new(bucket)}
	if _, err := client.HeadBucket(ctx, head); err == nil {
		return nil
	} else {
		var responseError *smithyhttp.ResponseError
		if !errors.As(err, &responseError) || responseError.HTTPStatusCode() != http.StatusNotFound {
			return fmt.Errorf("s3 bootstrap: check bucket %q: %w", bucket, err)
		}
	}
	region := client.Options().Region
	if region == "" {
		return errors.New("s3 bootstrap: AWS region is required to create a bucket")
	}
	input := &s3.CreateBucketInput{Bucket: new(bucket)}
	if config.BucketNamespace != nil {
		input.BucketNamespace = types.BucketNamespace(*config.BucketNamespace)
	}
	if region != "us-east-1" {
		input.CreateBucketConfiguration = &types.CreateBucketConfiguration{
			LocationConstraint: types.BucketLocationConstraint(region),
		}
	}
	if _, err := client.CreateBucket(ctx, input); err != nil {
		var apiError smithy.APIError
		if errors.As(err, &apiError) && apiError.ErrorCode() == "BucketAlreadyOwnedByYou" {
			if _, err := client.HeadBucket(ctx, head); err != nil {
				return fmt.Errorf("s3 bootstrap: check bucket %q after conflict: %w", bucket, err)
			}
			return nil
		}
		return fmt.Errorf("s3 bootstrap: create bucket %q: %w", bucket, err)
	}
	if err := s3.NewBucketExistsWaiter(client).Wait(ctx, head, 5*time.Minute); err != nil {
		return fmt.Errorf("s3 bootstrap: bucket %q was created but is not ready: %w", bucket, err)
	}
	if config.OwnershipControls != nil {
		_, err := client.PutBucketOwnershipControls(ctx, &s3.PutBucketOwnershipControlsInput{
			Bucket: new(bucket),
			OwnershipControls: &types.OwnershipControls{Rules: []types.OwnershipControlsRule{{
				ObjectOwnership: types.ObjectOwnership(config.OwnershipControls.ObjectOwnership),
			}}},
		})
		if err != nil {
			return fmt.Errorf("s3 bootstrap: bucket %q was created but ownership controls failed: %w",
				bucket, err)
		}
	}
	if access := config.PublicAccessBlock; access != nil {
		_, err := client.PutPublicAccessBlock(ctx, &s3.PutPublicAccessBlockInput{
			Bucket: new(bucket),
			PublicAccessBlockConfiguration: &types.PublicAccessBlockConfiguration{
				BlockPublicAcls:       new(aws.ToBool(access.BlockPublicAcls)),
				BlockPublicPolicy:     new(aws.ToBool(access.BlockPublicPolicy)),
				IgnorePublicAcls:      new(aws.ToBool(access.IgnorePublicAcls)),
				RestrictPublicBuckets: new(aws.ToBool(access.RestrictPublicBuckets)),
			},
		})
		if err != nil {
			return fmt.Errorf("s3 bootstrap: bucket %q was created but public access block failed: %w",
				bucket, err)
		}
	}
	if config.Tags != nil && len(*config.Tags) > 0 {
		tags := make([]types.Tag, 0, len(*config.Tags))
		for _, key := range slices.Sorted(maps.Keys(*config.Tags)) {
			tags = append(tags, types.Tag{Key: new(key), Value: aws.String((*config.Tags)[key])})
		}
		_, err := client.PutBucketTagging(ctx, &s3.PutBucketTaggingInput{
			Bucket: new(bucket), Tagging: &types.Tagging{TagSet: tags},
		})
		if err != nil {
			return fmt.Errorf("s3 bootstrap: bucket %q was created but tags failed: %w", bucket, err)
		}
	}
	if config.Versioning != nil {
		_, err := client.PutBucketVersioning(ctx, &s3.PutBucketVersioningInput{
			Bucket: new(bucket),
			VersioningConfiguration: &types.VersioningConfiguration{
				Status: types.BucketVersioningStatus(config.Versioning.Status),
			},
		})
		if err != nil {
			return fmt.Errorf("s3 bootstrap: bucket %q was created but versioning failed: %w", bucket, err)
		}
		if config.Versioning.Status == "Enabled" && versioningDelay > 0 {
			// S3 recommends waiting 15 minutes before writing objects after first enabling versioning.
			timer := time.NewTimer(versioningDelay)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return fmt.Errorf("s3 bootstrap: bucket %q was created but versioning propagation "+
					"wait failed: %w", bucket, ctx.Err())
			case <-timer.C:
			}
		}
	}
	return nil
}
