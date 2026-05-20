package parse

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPascalToKebab(t *testing.T) {
	cases := map[string]string{
		"":             "",
		"Name":         "name",
		"BucketName":   "bucket-name",
		"CIDRBlock":    "cidr-block",
		"S3Bucket":     "s3-bucket",
		"VPCEndpointID": "vpc-endpoint-id",
		"URL":          "url",
		"URLPath":      "url-path",
		"HTTPSProxy":   "https-proxy",
		"AssumeRole":   "assume-role",
		"RoleARN":      "role-arn",
		"ABCdef":       "ab-cdef",
		"Replicas3":    "replicas3",
		"Replicas3Min": "replicas3-min",
	}
	for in, want := range cases {
		t.Run(in, func(t *testing.T) {
			require.Equal(t, want, PascalToKebab(in))
		})
	}
}
