package goschema

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/runtime"
)

func TestConfigurationReadersRequireMatchingRegistration(t *testing.T) {
	readers := []struct {
		name string
		read func(string) (*runtime.LibrarySchema, []string, error)
	}{
		{"library", func(dir string) (*runtime.LibrarySchema, []string, error) {
			return Read(dir)
		}},
		{"configuration", func(dir string) (*runtime.LibrarySchema, []string, error) {
			return ReadLibraryConfiguration(dir)
		}},
		{"indexed library", func(dir string) (*runtime.LibrarySchema, []string, error) {
			schema, _, warnings, err := ReadWithIndex(dir)
			return schema, warnings, err
		}},
		{"indexed configuration", func(dir string) (*runtime.LibrarySchema, []string, error) {
			schema, _, warnings, err := ReadLibraryConfigurationWithIndex(dir)
			return schema, warnings, err
		}},
	}
	tests := []struct {
		name    string
		library string
		message string
	}{
		{
			"missing Library", "", "Library()",
		},
		{
			"missing registration", `func Library() *runtime.Library {
	return &runtime.Library{Compatibility: runtime.LibraryCompatibility{RequiredAPI: "1.0"}}
}`, "Library().Configuration",
		},
		{
			"nil registration", `func Library() *runtime.Library {
	return &runtime.Library{Compatibility: runtime.LibraryCompatibility{RequiredAPI: "1.0"},
		Configuration: nil}
}`, "Library().Configuration",
		},
		{
			"unreadable registration", `func Library() *runtime.Library {
	return &runtime.Library{Compatibility: runtime.LibraryCompatibility{RequiredAPI: "1.0"},
		Configuration: computedConfiguration()}
}`, "Library().Configuration",
		},
		{
			"unreadable constructor", `func Library() *runtime.Library {
	return &runtime.Library{Compatibility: runtime.LibraryCompatibility{RequiredAPI: "1.0"},
		Configuration: &cfg.ConfigurationType[*Configuration]{
		New: func() *Configuration { return constructor() },
	}}
}`, "Library().Configuration",
		},
		{
			"different identity", `func Library() *runtime.Library {
	return &runtime.Library{Compatibility: runtime.LibraryCompatibility{RequiredAPI: "1.0"},
		Configuration: &cfg.ConfigurationType[*OtherConfiguration]{
		New: func() *OtherConfiguration { return &OtherConfiguration{} },
	}}
}`, "disagrees",
		},
		{
			"different digest", `func Library() *runtime.Library {
	return &runtime.Library{Compatibility: runtime.LibraryCompatibility{RequiredAPI: "1.0"},
		Configuration: &cfg.ConfigurationType[*Configuration]{
		New: func() *Configuration {
			return &Configuration{Region: &cfg.String{Default: "other"}}
		},
	}}
}`, "disagrees",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := writeConfigurationRegistration(t, tt.library)
			for _, reader := range readers {
				t.Run(reader.name, func(t *testing.T) {
					_, _, err := reader.read(dir)
					require.ErrorContains(t, err, tt.message)
				})
			}
		})
	}

	dir := writeConfigurationRegistration(t, `func Library() *runtime.Library {
	return &runtime.Library{Compatibility: runtime.LibraryCompatibility{RequiredAPI: "1.0"},
		Configuration: LibraryConfiguration()}
}`)
	for _, reader := range readers {
		t.Run("matching "+reader.name, func(t *testing.T) {
			schema, warnings, err := reader.read(dir)
			require.NoError(t, err)
			assert.Empty(t, warnings)
			assert.Equal(t, "example.com/configtest.Configuration", schema.ConfigurationIdentity)
			assert.True(t, schema.HasConfiguration)
			assert.NotEmpty(t, schema.ConfigurationDigest)
		})
	}
}

func writeConfigurationRegistration(t *testing.T, library string) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"),
		[]byte("module example.com/configtest\n\ngo 1.26\n"), 0o644))
	source := `package configtest
import (
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
)
type Configuration struct { Region *cfg.String }
type OtherConfiguration struct { Region *cfg.String }
func LibraryConfiguration() *cfg.ConfigurationType[*Configuration] {
	return &cfg.ConfigurationType[*Configuration]{
		New: func() *Configuration {
			return &Configuration{Region: &cfg.String{Default: "initial"}}
		},
	}
}
` + library
	require.NoError(t, os.WriteFile(filepath.Join(dir, "library.go"), []byte(source), 0o644))
	return dir
}

func TestForwardedConfigurationReadersRequireMatchingRegistration(t *testing.T) {
	for _, test := range []struct {
		name, library, message string
	}{
		{
			name: "missing registration", message: "Library().Configuration",
			library: `func Library() *runtime.Library {
	return &runtime.Library{Compatibility: runtime.LibraryCompatibility{RequiredAPI: "1.0"}}
}`,
		},
		{
			name: "different identity", message: "disagrees",
			library: `func Library() *runtime.Library {
	return &runtime.Library{Compatibility: runtime.LibraryCompatibility{RequiredAPI: "1.0"},
		Configuration: &cfg.ConfigurationType[*OtherConfiguration]{
			New: func() *OtherConfiguration { return &OtherConfiguration{} },
		}}
}`,
		},
		{
			name: "different digest", message: "disagrees",
			library: `func Library() *runtime.Library {
	return &runtime.Library{Compatibility: runtime.LibraryCompatibility{RequiredAPI: "1.0"},
		Configuration: &cfg.ConfigurationType[*Configuration]{
			New: func() *Configuration {
				return &Configuration{Region: &cfg.String{Default: "other"}}
			},
		}}
}`,
		},
		{name: "matching", library: `func Library() *runtime.Library {
	return &runtime.Library{Compatibility: runtime.LibraryCompatibility{RequiredAPI: "1.0"},
		Configuration: LibraryConfiguration()}
}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			config := writeConfigurationRegistration(t, test.library)
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"),
				[]byte("module example.com/service\n\ngo 1.26\n"), 0o644))
			source := `package service
import (
	settings "example.com/configtest"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
)
func Library() *runtime.Library {
	return &runtime.Library{
		Compatibility: runtime.LibraryCompatibility{RequiredAPI: "1.0"},
		Configuration: LibraryConfiguration(),
	}
}
func LibraryConfiguration() *cfg.ConfigurationType[*settings.Configuration] {
	return settings.LibraryConfiguration()
}
`
			require.NoError(t, os.WriteFile(
				filepath.Join(dir, "library.go"), []byte(source), 0o644))
			roots := []ModuleRoot{{Path: "example.com/configtest", Dir: config}}
			for _, reader := range []struct {
				name string
				read func() (*runtime.LibrarySchema, error)
			}{
				{"library", func() (*runtime.LibrarySchema, error) {
					schema, _, err := Read(dir, roots...)
					return schema, err
				}},
				{"configuration", func() (*runtime.LibrarySchema, error) {
					schema, _, err := ReadLibraryConfiguration(dir, roots...)
					return schema, err
				}},
				{"indexed library", func() (*runtime.LibrarySchema, error) {
					schema, _, _, err := ReadWithIndex(dir, roots...)
					return schema, err
				}},
				{"indexed configuration", func() (*runtime.LibrarySchema, error) {
					schema, _, _, err := ReadLibraryConfigurationWithIndex(dir, roots...)
					return schema, err
				}},
			} {
				t.Run(reader.name, func(t *testing.T) {
					schema, err := reader.read()
					if test.message != "" {
						require.ErrorContains(t, err, test.message)
						require.ErrorContains(t, err, "example.com/configtest")
					} else {
						require.NoError(t, err)
						assert.Equal(t, "example.com/configtest.Configuration",
							schema.ConfigurationIdentity)
					}
				})
			}
		})
	}
}
