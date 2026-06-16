package runner

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
	"github.com/cloudboss/unobin/pkg/ubtest"
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

func configValue(table runtime.ConfigTable, alias, name string) any {
	return table[runtime.ConfigRef{Alias: alias, Name: name}]
}

func TestLoadConfigurationFixtures(t *testing.T) {
	ubtest.Run(t, "testdata/ub/configurations", func(
		name string, src []byte,
	) (string, []string) {
		config, err := parseConfigSource(name+".ub", src)
		if err != nil {
			return "", []string{err.Error()}
		}
		_, _, err = loadConfigurations(config, name+".ub", map[string]*runtime.Library{
			"aws": awsModuleWithConfig(),
		}, map[string]map[string]bool{"aws": {"default": true}})
		if err != nil {
			return "", []string{err.Error()}
		}
		return "", nil
	})
}

func TestLoadConfigurationsDecodesDefault(t *testing.T) {
	path := writeConfig(t, `
factory: {
  configurations: {
    aws {
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
	got := configValue(out, "aws", "default").(*awsConfig)
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
	require.Equal(t, "us-east-1", configValue(out, "aws", "default").(*awsConfig).Region.Value)
	require.Equal(t, "us-east-2", configValue(out, "aws", "east2").(*awsConfig).Region.Value)
	require.Equal(t, runtime.ConfigTable{
		{Alias: "aws", Name: "default"}: map[string]any{"region": "us-east-1"},
		{Alias: "aws", Name: "east2"}:   map[string]any{"region": "us-east-2"},
	}, raw)
}

func TestLoadConfigurationsAppliesDefaultsWhenAbsent(t *testing.T) {
	path := writeConfig(t, `
factory: {
  configurations: {
    aws {
      region: 'us-east-1'
    }
  }
}
`)
	out, _, err := loadConfigurations(parseTestConfig(t, path), path, map[string]*runtime.Library{
		"aws": awsModuleWithConfig(),
	}, nil)
	require.NoError(t, err)
	got := configValue(out, "aws", "default").(*awsConfig)
	require.Equal(t, "default-profile", got.Profile.Value)
}

func TestLoadConfigurationsAllowsAbsentConfigurations(t *testing.T) {
	out, _, err := loadConfigurations(nil, "", map[string]*runtime.Library{
		"aws": awsModuleWithConfig(),
	}, nil)
	require.NoError(t, err)
	require.NotContains(t, out, runtime.ConfigRef{Alias: "aws", Name: "default"})
}

func TestLoadConfigurationsAllowsBlockMissingForModule(t *testing.T) {
	path := writeConfig(t, `
factory: {
  configurations: {
    other { region: 'us-west-2' }
  }
}
`)
	out, _, err := loadConfigurations(parseTestConfig(t, path), path, map[string]*runtime.Library{
		"aws":   awsModuleWithConfig(),
		"other": awsModuleWithConfig(),
	}, nil)
	require.NoError(t, err)
	require.NotContains(t, out, runtime.ConfigRef{Alias: "aws", Name: "default"})
	require.Equal(t, "us-west-2", configValue(out, "other", "default").(*awsConfig).Region.Value)
}

func TestLoadConfigurationsErrorsOnUnknownImportAlias(t *testing.T) {
	path := writeConfig(t, `
factory: {
  configurations: {
    ghost {}
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
    aws {}
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
    east2: aws {
      region: 'us-east-2'
    }
  }
}
`)
	out, _, err := loadConfigurations(parseTestConfig(t, path), path, map[string]*runtime.Library{
		"aws": awsModuleWithConfig(),
	}, nil)
	require.NoError(t, err)
	require.Equal(t, "us-east-2", configValue(out, "aws", "east2").(*awsConfig).Region.Value)
}

func TestLoadConfigurationsErrorsOnInvalidValues(t *testing.T) {
	path := writeConfig(t, `
factory: {
  configurations: {
    aws {
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
    aws {
      region:  'us-east-1'
      profile: 'prod'
    }
    east2: aws {
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
	require.Len(t, out, 2)
	require.Equal(t, "us-east-1", configValue(out, "aws", "default").(*awsConfig).Region.Value)
	require.Equal(t, "us-east-2", configValue(out, "aws", "east2").(*awsConfig).Region.Value)

	require.Equal(t, configValue(out, "aws", "default").(*awsConfig).Region.Value,
		configValue(raw, "aws", "default").(map[string]any)["region"])
	require.Equal(t, "us-east-2", configValue(raw, "aws", "east2").(map[string]any)["region"])
}

func TestParseConfigRejectsInputReferenceInConfigurations(t *testing.T) {
	path := writeConfig(t, `
factory: {
  configurations: {
    aws {
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
    aws {
      region: 'us-east-1'
    }
    bad: aws {
      region: 12345
    }
  }
}
`)
	_, _, err := loadConfigurations(parseTestConfig(t, path), path, map[string]*runtime.Library{
		"aws": awsModuleWithConfig(),
	}, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "configuration.bad")
}

func TestLoadConfigurationsAcceptsFactoryNameOverride(t *testing.T) {
	path := writeConfig(t, `
factory: {
  configurations: {
    aws { region: 'us-east-1' }
    cluster: aws { region: 'us-east-2' }
  }
}
`)
	out, _, err := loadConfigurations(parseTestConfig(t, path), path, map[string]*runtime.Library{
		"aws": awsModuleWithConfig(),
	}, map[string]map[string]bool{"aws": {"default": true, "cluster": true}})
	require.NoError(t, err)
	require.Equal(t, "us-east-2", configValue(out, "aws", "cluster").(*awsConfig).Region.Value)
}

func TestLoadConfigurationsResolvesLocals(t *testing.T) {
	path := writeConfig(t, `
locals: { region: 'us-east-1', team: 'core' }

factory: {
  configurations: {
    aws {
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
	got := configValue(out, "aws", "default").(*awsConfig)
	require.Equal(t, "us-east-1", got.Region.Value)
	require.Equal(t, "core", got.Profile.Value)
	require.Equal(t, runtime.ConfigTable{
		{Alias: "aws", Name: "default"}: map[string]any{
			"region":  "us-east-1",
			"profile": "core",
		},
	}, raw)
}
