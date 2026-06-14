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
factory: {
  configurations: {
    aws.default: {
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

func TestLoadConfigurationsAcceptsSourceStack(t *testing.T) {
	path := writeConfig(t, `
stack: {
  locals: { default-region: 'us-east-1' }

  factory: {
    configurations: {
      aws {
        region: local.default-region
      }
      east2: aws {
        region: 'us-east-2'
      }
    }
  }

  state: local { path: '.unobin/state' }
  encryption: noop {}
}
`)

	out, raw, err := loadConfigurations(parseTestConfig(t, path), path, map[string]*runtime.Library{
		"aws": awsModuleWithConfig(),
	}, nil)
	require.NoError(t, err)
	require.Equal(t, "us-east-1", out["aws"]["default"].(*awsConfig).Region.Value)
	require.Equal(t, "us-east-2", out["aws"]["east2"].(*awsConfig).Region.Value)
	require.Equal(t, map[string]map[string]any{
		"aws": {
			"default": map[string]any{"region": "us-east-1"},
			"east2":   map[string]any{"region": "us-east-2"},
		},
	}, raw)
}

func TestLoadConfigurationsAppliesDefaultsWhenAbsent(t *testing.T) {
	path := writeConfig(t, `
factory: {
  configurations: {
    aws.default: {
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

func TestLoadConfigurationsAllowsAbsentConfigurations(t *testing.T) {
	out, _, err := loadConfigurations(nil, "", map[string]*runtime.Library{
		"aws": awsModuleWithConfig(),
	}, nil)
	require.NoError(t, err)
	require.Empty(t, out["aws"])
}

func TestLoadConfigurationsAllowsBlockMissingForModule(t *testing.T) {
	path := writeConfig(t, `
factory: {
  configurations: {
    other.default: { region: 'us-west-2' }
  }
}
`)
	out, _, err := loadConfigurations(parseTestConfig(t, path), path, map[string]*runtime.Library{
		"aws":   awsModuleWithConfig(),
		"other": awsModuleWithConfig(),
	}, nil)
	require.NoError(t, err)
	require.Empty(t, out["aws"])
	require.Equal(t, "us-west-2", out["other"]["default"].(*awsConfig).Region.Value)
}

func TestLoadConfigurationsErrorsOnUnknownImportAlias(t *testing.T) {
	path := writeConfig(t, `
factory: {
  configurations: {
    ghost.default: {}
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
factory: {
  configurations: {
    aws.default: {}
  }
}
`)
	_, _, err := loadConfigurations(parseTestConfig(t, path), path, map[string]*runtime.Library{
		"aws": awsModuleNoConfig(),
	}, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no configuration")
}

func TestLoadConfigurationsAllowsMissingDefault(t *testing.T) {
	path := writeConfig(t, `
factory: {
  configurations: {
    aws.east2: {
      region: 'us-east-2'
    }
  }
}
`)
	out, _, err := loadConfigurations(parseTestConfig(t, path), path, map[string]*runtime.Library{
		"aws": awsModuleWithConfig(),
	}, nil)
	require.NoError(t, err)
	require.Equal(t, "us-east-2", out["aws"]["east2"].(*awsConfig).Region.Value)
}

func TestLoadConfigurationsErrorsOnInvalidValues(t *testing.T) {
	path := writeConfig(t, `
factory: {
  configurations: {
    aws.default: {
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
factory: {
  configurations: {
    aws.default: {
      region:  'us-east-1'
      profile: 'prod'
    }
    aws.east2: {
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

func TestParseConfigRejectsInputReferenceInConfigurations(t *testing.T) {
	path := writeConfig(t, `
factory: {
  configurations: {
    aws.default: {
      region: var.region
    }
  }
}
`)
	_, err := parseConfigFile(path)
	require.Error(t, err)
	require.Contains(t, err.Error(),
		"config values must be static, but var.region is a reference")
}

func TestLoadConfigurationsErrorsWhenAnyAliasFailsToDecode(t *testing.T) {
	path := writeConfig(t, `
factory: {
  configurations: {
    aws.default: {
      region: 'us-east-1'
    }
    aws.bad: {
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

func TestLoadConfigurationsRejectsInternalName(t *testing.T) {
	path := writeConfig(t, `
factory: {
  configurations: {
    aws.default: { region: 'us-east-1' }
    aws.cluster: { region: 'us-east-1' }
  }
}
`)
	_, _, err := loadConfigurations(parseTestConfig(t, path), path, map[string]*runtime.Library{
		"aws": awsModuleWithConfig(),
	}, map[string]map[string]bool{"aws": {"cluster": true}})
	require.Error(t, err)
	require.Equal(t, path+": factory.configurations.aws.cluster: defined internally by the factory; "+
		"remove this entry from config.ub", err.Error())
}

func TestLoadConfigurationsResolvesLocals(t *testing.T) {
	path := writeConfig(t, `
locals: { region: 'us-east-1', team: 'core' }

factory: {
  configurations: {
    aws.default: {
      region:  local.region
      profile: local.team
    }
  }
}
`)
	out, raw, err := loadConfigurations(parseTestConfig(t, path), path, map[string]*runtime.Library{
		"aws": awsModuleWithConfig(),
	}, nil)
	require.NoError(t, err)
	got := out["aws"]["default"].(*awsConfig)
	require.Equal(t, "us-east-1", got.Region.Value)
	require.Equal(t, "core", got.Profile.Value)
	require.Equal(t, map[string]map[string]any{
		"aws": {"default": map[string]any{"region": "us-east-1", "profile": "core"}},
	}, raw)
}
