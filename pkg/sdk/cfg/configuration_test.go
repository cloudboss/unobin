package cfg

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConfigurationTypeNewReturnsFreshInstance(t *testing.T) {
	type Configuration struct {
		Region  String
		Profile *String
	}
	ct := &ConfigurationType[any]{
		Description: "AWS configuration",
		New: func() any {
			return &Configuration{
				Profile: &String{Default: "default"},
			}
		},
	}

	first := ct.New().(*Configuration)
	first.Region.Value = "us-east-1"

	second := ct.New().(*Configuration)
	require.Empty(t, second.Region.Value, "New must hand back a fresh instance each call")
	require.Equal(t, "default", second.Profile.Default)
}
