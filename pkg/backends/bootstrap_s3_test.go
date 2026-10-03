package backends

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func bootstrapS3Client(region, endpoint string) *s3.Client {
	return s3.NewFromConfig(aws.Config{
		Region:      region,
		Credentials: credentials.NewStaticCredentialsProvider("test", "test", ""),
		Retryer:     func() aws.Retryer { return aws.NopRetryer{} },
	}, func(o *s3.Options) {
		o.BaseEndpoint = new(endpoint)
		o.UsePathStyle = true
	})
}

func TestS3BootstrapDisabled(t *testing.T) {
	for _, config := range []*S3BootstrapConfig{nil, {}} {
		require.NoError(t, bootstrapS3Bucket(t.Context(), nil, "state", config, 0))
	}
}

func TestS3BootstrapReusesExistingBucket(t *testing.T) {
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	config := &S3BootstrapConfig{
		BucketNamespace: new("account-regional"),
		Versioning:      &S3BucketVersioning{Status: "Enabled"},
		Tags:            new(map[string]string{"requested": "creation-only"}),
	}
	require.NoError(t, bootstrapS3Bucket(t.Context(), bootstrapS3Client("us-east-1", server.URL),
		"existing-state", config, time.Hour))
	assert.Equal(t, []string{"HEAD /existing-state"}, requests)
}

func TestS3BootstrapCreatesBucketWithOptions(t *testing.T) {
	for _, region := range []string{"us-east-1", "eu-west-1"} {
		t.Run(region, func(t *testing.T) {
			var requests []string
			bodies := map[string][]byte{}
			var namespace string
			created := false
			server := httptest.NewServer(http.HandlerFunc(
				func(w http.ResponseWriter, r *http.Request) {
					requests = append(requests, r.Method+" "+r.URL.RequestURI())
					body, err := io.ReadAll(r.Body)
					assert.NoError(t, err)
					if r.Method == http.MethodHead && !created {
						w.WriteHeader(http.StatusNotFound)
						return
					}
					if r.Method == http.MethodPut {
						bodies[strings.TrimSuffix(r.URL.RawQuery, "=")] = body
						if r.URL.RawQuery == "" {
							created = true
							namespace = r.Header.Get("x-amz-bucket-namespace")
						}
					}
					w.WriteHeader(http.StatusOK)
				}))
			defer server.Close()
			config := &S3BootstrapConfig{
				BucketNamespace: new("account-regional"),
				Versioning:      &S3BucketVersioning{Status: "Suspended"},
				PublicAccessBlock: &S3BucketPublicAccessBlock{
					BlockPublicAcls: new(true), BlockPublicPolicy: new(false),
					IgnorePublicAcls: new(true), RestrictPublicBuckets: new(true),
				},
				OwnershipControls: &S3BucketOwnershipControls{
					ObjectOwnership: "BucketOwnerEnforced",
				},
				Tags: new(map[string]string{"purpose": "state", "environment": "dev"}),
			}
			require.NoError(t, bootstrapS3Bucket(t.Context(),
				bootstrapS3Client(region, server.URL), "acme-state", config, 0))
			require.GreaterOrEqual(t, len(requests), 3)
			assert.Equal(t, []string{"HEAD /acme-state", "PUT /acme-state",
				"HEAD /acme-state"}, requests[:3])
			assert.ElementsMatch(t, []string{"PUT /acme-state?tagging=",
				"PUT /acme-state?ownershipControls=", "PUT /acme-state?publicAccessBlock=",
				"PUT /acme-state?versioning="}, requests[3:])
			assert.Equal(t, "account-regional", namespace)
			if region == "us-east-1" {
				assert.Empty(t, bodies[""])
			} else {
				var create struct{ LocationConstraint string }
				require.NoError(t, xml.Unmarshal(bodies[""], &create))
				assert.Equal(t, region, create.LocationConstraint)
			}
			var versioning struct{ Status string }
			require.NoError(t, xml.Unmarshal(bodies["versioning"], &versioning))
			assert.Equal(t, "Suspended", versioning.Status)
			var access struct {
				BlockPublicAcls, BlockPublicPolicy, IgnorePublicAcls, RestrictPublicBuckets bool
			}
			require.NoError(t, xml.Unmarshal(bodies["publicAccessBlock"], &access))
			assert.True(t, access.BlockPublicAcls)
			assert.False(t, access.BlockPublicPolicy)
			assert.Contains(t, string(bodies["publicAccessBlock"]),
				"<BlockPublicPolicy>false</BlockPublicPolicy>")
			assert.True(t, access.IgnorePublicAcls)
			assert.True(t, access.RestrictPublicBuckets)
			var ownership struct {
				Rule struct{ ObjectOwnership string }
			}
			require.NoError(t, xml.Unmarshal(bodies["ownershipControls"], &ownership))
			assert.Equal(t, "BucketOwnerEnforced", ownership.Rule.ObjectOwnership)
			var tags struct {
				Tags []struct{ Key, Value string } `xml:"TagSet>Tag"`
			}
			require.NoError(t, xml.Unmarshal(bodies["tagging"], &tags))
			gotTags := map[string]string{}
			for _, tag := range tags.Tags {
				gotTags[tag.Key] = tag.Value
			}
			assert.Equal(t, *config.Tags, gotTags)
		})
	}
}

func TestS3BootstrapFailuresAndCreationRace(t *testing.T) {
	tests := []struct {
		name       string
		headStatus int
		createCode string
		want       []string
		wantError  string
	}{
		{
			name: "access denied", headStatus: http.StatusForbidden,
			want: []string{"HEAD"}, wantError: "check bucket",
		},
		{
			name: "another creator owns our bucket", headStatus: http.StatusNotFound,
			createCode: "BucketAlreadyOwnedByYou", want: []string{"HEAD", "PUT", "HEAD"},
		},
		{
			name: "name belongs to another account", headStatus: http.StatusNotFound,
			createCode: "BucketAlreadyExists", want: []string{"HEAD", "PUT"},
			wantError: "BucketAlreadyExists",
		},
		{
			name: "cannot create", headStatus: http.StatusNotFound,
			createCode: "AccessDenied", want: []string{"HEAD", "PUT"},
			wantError: "create bucket",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var requests []string
			server := httptest.NewServer(http.HandlerFunc(
				func(w http.ResponseWriter, r *http.Request) {
					requests = append(requests, r.Method)
					if len(requests) == 1 {
						w.WriteHeader(tt.headStatus)
						return
					}
					if r.Method == http.MethodPut {
						w.Header().Set("Content-Type", "application/xml")
						w.WriteHeader(http.StatusConflict)
						fmt.Fprintf(w, "<Error><Code>%s</Code></Error>", tt.createCode)
						return
					}
					w.WriteHeader(http.StatusOK)
				}))
			defer server.Close()
			err := bootstrapS3Bucket(t.Context(), bootstrapS3Client("us-east-1", server.URL),
				"state", &S3BootstrapConfig{Versioning: &S3BucketVersioning{Status: "Enabled"}}, 0)
			if tt.wantError == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tt.wantError)
			}
			assert.Equal(t, tt.want, requests)
		})
	}
}

func TestS3BootstrapRetainsBucketAfterConfigurationFailure(t *testing.T) {
	created := false
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		if r.Method == http.MethodHead && !created {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.URL.Query().Has("publicAccessBlock") {
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, "<Error><Code>AccessDenied</Code></Error>")
			return
		}
		if r.Method == http.MethodPut {
			created = true
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	client := bootstrapS3Client("us-east-1", server.URL)
	config := &S3BootstrapConfig{
		PublicAccessBlock: &S3BucketPublicAccessBlock{BlockPublicAcls: new(true)},
	}
	err := bootstrapS3Bucket(t.Context(), client, "state", config, 0)
	require.ErrorContains(t, err, "was created")
	require.ErrorContains(t, err, "public access block")
	require.NoError(t, bootstrapS3Bucket(t.Context(), client, "state", config, 0))
	assert.Equal(t, []string{"HEAD /state", "PUT /state", "HEAD /state",
		"PUT /state?publicAccessBlock=", "HEAD /state"}, requests)
}

func TestS3BootstrapVersioningWaitCanBeCanceled(t *testing.T) {
	created := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead && !created {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Method == http.MethodPut {
			created = true
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	err := bootstrapS3Bucket(ctx, bootstrapS3Client("us-east-1", server.URL), "state",
		&S3BootstrapConfig{Versioning: &S3BucketVersioning{Status: "Enabled"}}, time.Hour)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.ErrorContains(t, err, "versioning propagation")
}
