package cloud

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/awscfg"
)

func str(v string) *string {
	out := v
	return &out
}

func TestDescribeReportsAssumeRole(t *testing.T) {
	a := &DescribeAction{Label: "x"}
	out, err := a.Run(t.Context(), &awscfg.Configuration{
		Region: str("us-east-2"),
		AssumeRole: &awscfg.AssumeRole{
			RoleArn: "arn:aws:iam::123456789012:role/unobin-example",
		},
	})
	require.NoError(t, err)
	require.Equal(t, &DescribeActionOutput{
		Label:   "x",
		Region:  "us-east-2",
		RoleArn: "arn:aws:iam::123456789012:role/unobin-example",
		Source:  "assume-role",
	}, out)
}

func TestDescribeReportsAmbientWithoutConfiguration(t *testing.T) {
	a := &DescribeAction{Label: "x"}
	out, err := a.Run(t.Context(), nil)
	require.NoError(t, err)
	require.Equal(t, &DescribeActionOutput{
		Label:  "x",
		Region: "default",
		Source: "ambient",
	}, out)
}
