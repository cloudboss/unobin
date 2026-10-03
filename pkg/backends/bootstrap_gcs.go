package backends

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"google.golang.org/api/googleapi"
	"google.golang.org/api/storage/v1"
)

type GCSBootstrapConfig struct {
	Location         *string
	Versioning       *GCSBucketVersioning
	IAMConfiguration *GCSBucketIAMConfiguration
	Labels           *map[string]string
}

type GCSBucketVersioning struct {
	Enabled bool
}

type GCSBucketIAMConfiguration struct {
	PublicAccessPrevention   *string
	UniformBucketLevelAccess *GCSBucketUniformBucketLevelAccess
}

type GCSBucketUniformBucketLevelAccess struct {
	Enabled bool
}

func (c *GCSBootstrapConfig) enabled() bool {
	return c != nil && *c != (GCSBootstrapConfig{})
}

func (c *GCSBootstrapConfig) validate() error {
	if !c.enabled() {
		return nil
	}
	if c.IAMConfiguration != nil && c.IAMConfiguration.PublicAccessPrevention != nil {
		switch *c.IAMConfiguration.PublicAccessPrevention {
		case "enforced", "inherited":
		default:
			return errors.New("gcs bootstrap.iam-configuration.public-access-prevention must be " +
				"enforced or inherited")
		}
	}
	return nil
}

func bootstrapGCSBucket(
	ctx context.Context,
	service *storage.Service,
	bucket, project string,
	config *GCSBootstrapConfig,
) error {
	if !config.enabled() {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	if _, err := service.Buckets.Get(bucket).Context(ctx).Do(); err == nil {
		return nil
	} else {
		var apiError *googleapi.Error
		if !errors.As(err, &apiError) || apiError.Code != http.StatusNotFound {
			return fmt.Errorf("gcs bootstrap: check bucket %q: %w", bucket, err)
		}
	}
	if project == "" {
		return errors.New("gcs bootstrap: gcp.project is required to create a bucket")
	}
	if config.Location == nil || *config.Location == "" {
		return errors.New("gcs bootstrap: bootstrap.location is required to create a bucket")
	}
	input := &storage.Bucket{Name: bucket, Location: *config.Location}
	if config.Labels != nil {
		input.Labels = *config.Labels
	}
	if config.Versioning != nil {
		input.Versioning = &storage.BucketVersioning{
			Enabled: config.Versioning.Enabled, ForceSendFields: []string{"Enabled"},
		}
	}
	if iam := config.IAMConfiguration; iam != nil {
		input.IamConfiguration = &storage.BucketIamConfiguration{
			PublicAccessPrevention: optString(iam.PublicAccessPrevention),
		}
		if access := iam.UniformBucketLevelAccess; access != nil {
			input.IamConfiguration.UniformBucketLevelAccess =
				&storage.BucketIamConfigurationUniformBucketLevelAccess{
					Enabled: access.Enabled, ForceSendFields: []string{"Enabled"},
				}
		}
	}
	if _, err := service.Buckets.Insert(project, input).Context(ctx).Do(); err != nil {
		var apiError *googleapi.Error
		if errors.As(err, &apiError) && apiError.Code == http.StatusConflict {
			if _, err := service.Buckets.Get(bucket).Context(ctx).Do(); err != nil {
				return fmt.Errorf("gcs bootstrap: check bucket after conflict %q: %w", bucket, err)
			}
			return nil
		}
		return fmt.Errorf("gcs bootstrap: create bucket %q: %w", bucket, err)
	}
	return nil
}
