package cfg

import (
	"testing"

	"github.com/stretchr/testify/require"

	encodedvalue "github.com/cloudboss/unobin/pkg/encoding/value"
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

func TestConfigurationTypeExposesVersionAndMigration(t *testing.T) {
	migration := func(
		oldVersion int,
		value encodedvalue.Value,
	) (encodedvalue.Value, error) {
		require.Equal(t, 1, oldVersion)
		return value, nil
	}
	configuration := &ConfigurationType[*struct{}]{
		SchemaVersion: 2,
		New:           func() *struct{} { return &struct{}{} },
		Migrate:       migration,
	}

	require.Equal(t, 2, configuration.SchemaVersionNumber())
	got, err := configuration.Migration()(1, encodedvalue.String("old"))
	require.NoError(t, err)
	require.Equal(t, encodedvalue.String("old"), got)
}

func TestNilConfigurationTypeHasNoVersionOrMigration(t *testing.T) {
	var configuration *ConfigurationType[*struct{}]

	require.Zero(t, configuration.SchemaVersionNumber())
	require.Nil(t, configuration.Migration())
}
