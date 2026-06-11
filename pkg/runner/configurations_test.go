package runner

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
)

type awsAssumeRole struct {
	RoleArn    cfg.String
	ExternalId *cfg.String
}

type awsConfig struct {
	Region     cfg.String
	Profile    *cfg.String
	AssumeRole *awsAssumeRole
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
	}, nil, nil)
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
	}, nil, nil)
	require.NoError(t, err)
	got := out["aws"]["default"].(*awsConfig)
	require.Equal(t, "default-profile", got.Profile.Value)
}

func TestLoadConfigurationsAllowsAbsentConfigurations(t *testing.T) {
	out, _, err := loadConfigurations(nil, "", map[string]*runtime.Library{
		"aws": awsModuleWithConfig(),
	}, nil, nil)
	require.NoError(t, err)
	require.Empty(t, out["aws"])
}

func TestLoadConfigurationsAllowsBlockMissingForModule(t *testing.T) {
	path := writeConfig(t, `
configurations: {
  other: {
    default: { region: 'us-west-2' }
  }
}
`)
	out, _, err := loadConfigurations(parseTestConfig(t, path), path, map[string]*runtime.Library{
		"aws":   awsModuleWithConfig(),
		"other": awsModuleWithConfig(),
	}, nil, nil)
	require.NoError(t, err)
	require.Empty(t, out["aws"])
	require.Equal(t, "us-west-2", out["other"]["default"].(*awsConfig).Region.Value)
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
		parseTestConfig(t, path), path, map[string]*runtime.Library{}, nil, nil)
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
	}, nil, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no configuration")
}

func TestLoadConfigurationsAllowsMissingDefault(t *testing.T) {
	path := writeConfig(t, `
configurations: {
  aws: {
    east2: {
      region: 'us-east-2'
    }
  }
}
`)
	out, _, err := loadConfigurations(parseTestConfig(t, path), path, map[string]*runtime.Library{
		"aws": awsModuleWithConfig(),
	}, nil, nil)
	require.NoError(t, err)
	require.Equal(t, "us-east-2", out["aws"]["east2"].(*awsConfig).Region.Value)
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
	}, nil, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "region")
}

func TestLoadConfigurationsReturnsNilWhenNoModuleNeedsOne(t *testing.T) {
	out, _, err := loadConfigurations(nil, "", map[string]*runtime.Library{
		"core": awsModuleNoConfig(),
	}, nil, nil)
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
	}, nil, nil)
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
	}, map[string]any{"region": "us-east-1", "env": "prod"}, nil)
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
	}, map[string]any{"region": "us-east-1"}, nil)
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
	}, nil, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "configurations.aws.bad")
}

func TestLoadConfigurationsRejectsInternalName(t *testing.T) {
	path := writeConfig(t, `
configurations: {
  aws: {
    default: { region: 'us-east-1' }
    cluster: { region: 'us-east-1' }
  }
}
`)
	_, _, err := loadConfigurations(parseTestConfig(t, path), path, map[string]*runtime.Library{
		"aws": awsModuleWithConfig(),
	}, nil, map[string]map[string]bool{"aws": {"cluster": true}})
	require.Error(t, err)
	require.Equal(t, path+": configurations.aws.cluster: defined internally by the factory; "+
		"remove this entry from config.ub", err.Error())
}

func TestLoadConfigurationsResolvesLocals(t *testing.T) {
	path := writeConfig(t, `
locals: { region: 'us-east-1' }

configurations: {
  aws: {
    default: {
      region:  local.region
      profile: var.team
    }
  }
}
`)
	out, raw, err := loadConfigurations(parseTestConfig(t, path), path, map[string]*runtime.Library{
		"aws": awsModuleWithConfig(),
	}, map[string]any{"team": "core"}, nil)
	require.NoError(t, err)
	got := out["aws"]["default"].(*awsConfig)
	require.Equal(t, "us-east-1", got.Region.Value)
	require.Equal(t, "core", got.Profile.Value)
	require.Equal(t, map[string]map[string]any{
		"aws": {"default": map[string]any{"region": "us-east-1", "profile": "core"}},
	}, raw)
}
