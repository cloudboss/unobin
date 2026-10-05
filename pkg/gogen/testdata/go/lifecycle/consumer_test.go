package testmod

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"example.com/testmod/resources"
)

func TestImplementedLifecycle(t *testing.T) {
	resource := resources.S3Bucket{}
	output, err := resource.Create(context.Background(), nil)
	require.NoError(t, err)
	require.Equal(t, "implemented", output.Arn)
}
