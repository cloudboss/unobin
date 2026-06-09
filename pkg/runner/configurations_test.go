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

func awsModuleWithConfig() *runtime.Library {
	return &runtime.Library{
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

func awsModuleNoConfig() *runtime.Library {
	return &runtime.Library{Name: "aws"}
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
	out, _, err := loadConfigurations(parseTestConfig(t, path), path, map[string]*runtime.Library{
		"aws": awsModuleWithConfig(),
	}, nil)
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
	out, _, err := loadConfigurations(parseTestConfig(t, path), path, map[string]*runtime.Library{
		"aws": awsModuleWithConfig(),
	}, nil)
	require.NoError(t, err)
	got := out["aws"]["default"].(*awsConfig)
	require.Equal(t, "default-profile", got.Profile.Value)
}

func TestLoadConfigurationsErrorsWhenModuleRequiresOneAndConfigIsAbsent(t *testing.T) {
	_, _, err := loadConfigurations(nil, "", map[string]*runtime.Library{
		"aws": awsModuleWithConfig(),
	}, nil)
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
	_, _, err := loadConfigurations(parseTestConfig(t, path), path, map[string]*runtime.Library{
		"aws": awsModuleWithConfig(),
	}, nil)
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
	_, _, err := loadConfigurations(
		parseTestConfig(t, path), path, map[string]*runtime.Library{}, nil)
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
	_, _, err := loadConfigurations(parseTestConfig(t, path), path, map[string]*runtime.Library{
		"aws": awsModuleNoConfig(),
	}, nil)
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
	_, _, err := loadConfigurations(parseTestConfig(t, path), path, map[string]*runtime.Library{
		"aws": awsModuleWithConfig(),
	}, nil)
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
	_, _, err := loadConfigurations(parseTestConfig(t, path), path, map[string]*runtime.Library{
		"aws": awsModuleWithConfig(),
	}, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "region")
}

func TestLoadConfigurationsReturnsNilWhenNoModuleNeedsOne(t *testing.T) {
	out, _, err := loadConfigurations(nil, "", map[string]*runtime.Library{
		"core": awsModuleNoConfig(),
	}, nil)
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
	out, raw, err := loadConfigurations(parseTestConfig(t, path), path, map[string]*runtime.Library{
		"aws": awsModuleWithConfig(),
	}, nil)
	require.NoError(t, err)
	require.Len(t, out["aws"], 2)
	require.Equal(t, "us-east-1", out["aws"]["default"].(*awsConfig).Region.Value)
	require.Equal(t, "us-east-2", out["aws"]["east2"].(*awsConfig).Region.Value)

	require.Equal(t, out["aws"]["default"].(*awsConfig).Region.Value,
		raw["aws"]["default"].(map[string]any)["region"])
	require.Equal(t, "us-east-2", raw["aws"]["east2"].(map[string]any)["region"])
}

func TestLoadConfigurationsResolvesInputReferences(t *testing.T) {
	path := writeConfig(t, `
configurations: {
  aws: {
    default: {
      region:  var.region
      profile: $'{{var.env}}-profile'
    }
  }
}
`)
	out, raw, err := loadConfigurations(parseTestConfig(t, path), path, map[string]*runtime.Library{
		"aws": awsModuleWithConfig(),
	}, map[string]any{"region": "us-east-1", "env": "prod"})
	require.NoError(t, err)
	got := out["aws"]["default"].(*awsConfig)
	require.Equal(t, "us-east-1", got.Region.Value)
	require.Equal(t, "prod-profile", got.Profile.Value)
	require.Equal(t, "us-east-1", raw["aws"]["default"].(map[string]any)["region"])
	require.Equal(t, "prod-profile", raw["aws"]["default"].(map[string]any)["profile"])
}

func TestLoadConfigurationsErrorsOnUnknownInput(t *testing.T) {
	path := writeConfig(t, `
configurations: {
  aws: {
    default: {
      region: var.missing
    }
  }
}
`)
	_, _, err := loadConfigurations(parseTestConfig(t, path), path, map[string]*runtime.Library{
		"aws": awsModuleWithConfig(),
	}, map[string]any{"region": "us-east-1"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "configurations.aws.default")
	require.Contains(t, err.Error(), "var.missing")
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
	_, _, err := loadConfigurations(parseTestConfig(t, path), path, map[string]*runtime.Library{
		"aws": awsModuleWithConfig(),
	}, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "configurations.aws.bad")
}
