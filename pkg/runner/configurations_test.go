package runner

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
)

type awsConfig struct {
	Region  cfg.String
	Profile *cfg.String
}

func awsModuleWithConfig() *runtime.Module {
	return &runtime.Module{
		Name: "aws",
		Configuration: &cfg.ConfigurationType{
			New: func() any {
				return &awsConfig{
					Profile: &cfg.String{Default: "default-profile"},
				}
			},
		},
	}
}

func awsModuleNoConfig() *runtime.Module {
	return &runtime.Module{Name: "aws"}
}

func TestLoadConfigurationsDecodesDefault(t *testing.T) {
	path := writeConfig(t, `
configurations: {
  aws: {
    default: {
      region:  'us-east-1'
      profile: 'prod'
    }
  }
}
`)
	out, _, err := loadConfigurations(path, map[string]*runtime.Module{
		"aws": awsModuleWithConfig(),
	})
	require.NoError(t, err)
	got := out["aws"]["default"].(*awsConfig)
	require.Equal(t, "us-east-1", got.Region.Value)
	require.Equal(t, "prod", got.Profile.Value)
}

func TestLoadConfigurationsAppliesDefaultsWhenAbsent(t *testing.T) {
	path := writeConfig(t, `
configurations: {
  aws: {
    default: {
      region: 'us-east-1'
    }
  }
}
`)
	out, _, err := loadConfigurations(path, map[string]*runtime.Module{
		"aws": awsModuleWithConfig(),
	})
	require.NoError(t, err)
	got := out["aws"]["default"].(*awsConfig)
	require.Equal(t, "default-profile", got.Profile.Value)
}

func TestLoadConfigurationsErrorsWhenModuleRequiresOneAndConfigIsAbsent(t *testing.T) {
	_, _, err := loadConfigurations("", map[string]*runtime.Module{
		"aws": awsModuleWithConfig(),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "configurations.aws")
}

func TestLoadConfigurationsErrorsWhenBlockMissingForModule(t *testing.T) {
	path := writeConfig(t, `
configurations: {
  other: {
    default: {}
  }
}
`)
	_, _, err := loadConfigurations(path, map[string]*runtime.Module{
		"aws": awsModuleWithConfig(),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "configurations.aws")
}

func TestLoadConfigurationsErrorsOnUnknownImportAlias(t *testing.T) {
	path := writeConfig(t, `
configurations: {
  ghost: {
    default: {}
  }
}
`)
	_, _, err := loadConfigurations(path, map[string]*runtime.Module{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "ghost")
	require.Contains(t, err.Error(), "unknown import alias")
}

func TestLoadConfigurationsErrorsWhenModuleHasNoConfiguration(t *testing.T) {
	path := writeConfig(t, `
configurations: {
  aws: {
    default: {}
  }
}
`)
	_, _, err := loadConfigurations(path, map[string]*runtime.Module{
		"aws": awsModuleNoConfig(),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "no configuration")
}

func TestLoadConfigurationsErrorsWhenDefaultMissing(t *testing.T) {
	path := writeConfig(t, `
configurations: {
  aws: {
    east2: {
      region: 'us-east-2'
    }
  }
}
`)
	_, _, err := loadConfigurations(path, map[string]*runtime.Module{
		"aws": awsModuleWithConfig(),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "default")
}

func TestLoadConfigurationsErrorsOnInvalidValues(t *testing.T) {
	path := writeConfig(t, `
configurations: {
  aws: {
    default: {
      region: 12345
    }
  }
}
`)
	_, _, err := loadConfigurations(path, map[string]*runtime.Module{
		"aws": awsModuleWithConfig(),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "region")
}

func TestLoadConfigurationsReturnsNilWhenNoModuleNeedsOne(t *testing.T) {
	out, _, err := loadConfigurations("", map[string]*runtime.Module{
		"core": awsModuleNoConfig(),
	})
	require.NoError(t, err)
	require.Empty(t, out)
}

func TestLoadConfigurationsDecodesMultipleAliases(t *testing.T) {
	path := writeConfig(t, `
configurations: {
  aws: {
    default: {
      region:  'us-east-1'
      profile: 'prod'
    }
    east2: {
      region:  'us-east-2'
      profile: 'prod'
    }
  }
}
`)
	out, raw, err := loadConfigurations(path, map[string]*runtime.Module{
		"aws": awsModuleWithConfig(),
	})
	require.NoError(t, err)
	require.Len(t, out["aws"], 2)
	require.Equal(t, "us-east-1", out["aws"]["default"].(*awsConfig).Region.Value)
	require.Equal(t, "us-east-2", out["aws"]["east2"].(*awsConfig).Region.Value)

	require.Equal(t, out["aws"]["default"].(*awsConfig).Region.Value,
		raw["aws"]["default"].(map[string]any)["region"])
	require.Equal(t, "us-east-2", raw["aws"]["east2"].(map[string]any)["region"])
}

func TestLoadConfigurationsErrorsWhenAnyAliasFailsToDecode(t *testing.T) {
	path := writeConfig(t, `
configurations: {
  aws: {
    default: {
      region: 'us-east-1'
    }
    bad: {
      region: 12345
    }
  }
}
`)
	_, _, err := loadConfigurations(path, map[string]*runtime.Module{
		"aws": awsModuleWithConfig(),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "configurations.aws.bad")
}
