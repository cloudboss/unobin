package runtime

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
)

const configurationLibraryPath = "example.com/cloud"

type recordedConfiguration struct {
	Endpoint    string
	Credentials struct {
		Token string
		Empty []string
	}
	Servers  []string
	Labels   map[string]string
	Optional *string
	Nullable *string
}

func configurationRegistration(
	version int,
	migrate cfg.ConfigurationMigrationFunc,
) *cfg.ConfigurationType[*recordedConfiguration] {
	return &cfg.ConfigurationType[*recordedConfiguration]{
		SchemaVersion: version,
		New: func() *recordedConfiguration {
			return &recordedConfiguration{}
		},
		Migrate: migrate,
	}
}

func testConfigurationValue(t *testing.T) EncodedValue {
	t.Helper()
	empty, err := ListValue(nil)
	require.NoError(t, err)
	credentials, err := ObjectValue(map[string]EncodedValue{
		"empty": empty,
		"token": StringValue("secret"),
	})
	require.NoError(t, err)
	servers, err := ListValue([]EncodedValue{
		StringValue("first"),
		StringValue("second"),
	})
	require.NoError(t, err)
	labels, err := MapValue(map[string]EncodedValue{
		"01":    StringValue("numeric-secret"),
		"a/b~":  StringValue("label-secret"),
		"plain": StringValue("visible"),
	})
	require.NoError(t, err)
	value, err := ObjectValue(map[string]EncodedValue{
		"credentials": credentials,
		"endpoint":    StringValue("https://api.example"),
		"labels":      labels,
		"nullable":    NullValue(),
		"optional":    AbsentValue(),
		"servers":     servers,
	})
	require.NoError(t, err)
	return value
}

func deterministicIDs(ids ...string) func() (string, error) {
	index := 0
	return func() (string, error) {
		if index >= len(ids) {
			return "", errors.New("no sensitive ID available")
		}
		id := ids[index]
		index++
		return id, nil
	}
}

func validConfigurationRecord(t *testing.T) ConfigurationRecord {
	t.Helper()
	definition, err := resolveConfigurationDefinition(
		configurationLibraryPath,
		configurationRegistration(1, nil),
	)
	require.NoError(t, err)
	record, err := definition.newConfigurationRecordWith(
		"library-config.cloud",
		testConfigurationValue(t),
		[]string{"/servers/1", "/credentials", "/nullable", "/labels/a~1b~0"},
		nil,
		deterministicIDs(
			strings.Repeat("1", 32),
			strings.Repeat("2", 32),
			strings.Repeat("3", 32),
			strings.Repeat("4", 32),
		),
	)
	require.NoError(t, err)
	return record
}

func cloneConfigurationRecord(record ConfigurationRecord) ConfigurationRecord {
	record.SensitivePaths = append([]string{}, record.SensitivePaths...)
	record.SensitiveValues = append([]SensitiveValueRecord{}, record.SensitiveValues...)
	return record
}

func TestResolveConfigurationDefinition(t *testing.T) {
	definition, err := resolveConfigurationDefinition(
		configurationLibraryPath,
		configurationRegistration(2, nil),
	)
	require.NoError(t, err)
	require.Equal(t, configurationLibraryPath, definition.libraryPath)
	require.Equal(t, 2, definition.schemaVersion)
	require.NotEmpty(t, definition.schemaDigest)
	require.False(t, definition.noConfig)

	noConfig, err := resolveConfigurationDefinition(configurationLibraryPath, nil)
	require.NoError(t, err)
	require.Equal(t, 1, noConfig.schemaVersion)
	require.NotEmpty(t, noConfig.schemaDigest)
	require.True(t, noConfig.noConfig)
}

func TestResolveConfigurationDefinitionUsesCompiledSchema(t *testing.T) {
	registration := configurationRegistration(1, nil)
	view, err := cfg.View(registration)
	require.NoError(t, err)
	constraints := []lang.ConstraintSpec{{
		Kind:    "predicate",
		When:    "true",
		Require: "input.endpoint != ''",
		Message: "endpoint is required",
	}}
	digest := cfg.DigestView(view.Fields, view.Defaults, constraints)
	library := &Library{
		Configuration: registration,
		Schema: &LibrarySchema{
			ConfigurationFields:      view.Fields,
			ConfigurationDefaults:    view.Defaults,
			ConfigurationConstraints: constraints,
			ConfigurationDigest:      digest,
			HasConfiguration:         true,
		},
	}
	definition, err := resolveLibraryConfigurationDefinition(
		configurationLibraryPath,
		library,
	)
	require.NoError(t, err)
	require.Equal(t, digest, definition.schemaDigest)

	fields, ok := testConfigurationValue(t).ObjectFields()
	require.True(t, ok)
	fields["endpoint"] = StringValue("")
	value, err := ObjectValue(fields)
	require.NoError(t, err)
	_, err = definition.newConfigurationRecord("library-config.cloud", value, nil, nil)
	require.ErrorContains(t, err, "endpoint is required")
}

func TestResolveConfigurationDefinitionRejectsInvalidRegistration(t *testing.T) {
	tests := []struct {
		name         string
		libraryPath  string
		registration cfg.Registration
		message      string
	}{
		{
			name:         "library path",
			registration: configurationRegistration(1, nil),
			message:      "library path is required",
		},
		{
			name:         "schema version",
			libraryPath:  configurationLibraryPath,
			registration: configurationRegistration(0, nil),
			message:      "schema version must be greater than zero",
		},
		{
			name:        "missing constructor",
			libraryPath: configurationLibraryPath,
			registration: &cfg.ConfigurationType[*recordedConfiguration]{
				SchemaVersion: 1,
			},
			message: "ConfigurationType.New returned nil",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := resolveConfigurationDefinition(tt.libraryPath, tt.registration)
			require.ErrorContains(t, err, tt.message)
		})
	}
}

func TestNewConfigurationRecordAssignsSensitiveLeafIDs(t *testing.T) {
	definition, err := resolveConfigurationDefinition(
		configurationLibraryPath,
		configurationRegistration(1, nil),
	)
	require.NoError(t, err)
	record, err := definition.newConfigurationRecordWith(
		"library-config.cloud",
		testConfigurationValue(t),
		[]string{
			"/servers/1",
			"/credentials",
			"/credentials",
			"/nullable",
			"/labels/a~1b~0",
		},
		nil,
		deterministicIDs(
			strings.Repeat("1", 32),
			strings.Repeat("2", 32),
			strings.Repeat("3", 32),
			strings.Repeat("4", 32),
		),
	)
	require.NoError(t, err)
	require.Equal(t, "library-config.cloud", record.Address)
	require.Equal(t, configurationLibraryPath, record.LibraryPath)
	require.Equal(t, 1, record.SchemaVersion)
	require.Equal(t, definition.schemaDigest, record.SchemaDigest)
	require.Equal(t, []string{
		"/credentials",
		"/labels/a~1b~0",
		"/nullable",
		"/servers/1",
	}, record.SensitivePaths)
	require.Equal(t, []SensitiveValueRecord{
		{Path: "/credentials/token", ID: strings.Repeat("1", 32)},
		{Path: "/labels/a~1b~0", ID: strings.Repeat("2", 32)},
		{Path: "/nullable", ID: strings.Repeat("3", 32)},
		{Path: "/servers/1", ID: strings.Repeat("4", 32)},
	}, record.SensitiveValues)
	require.Equal(
		t,
		"ecc2ceeb7b7a6350857c884da43a96c194f5f82115636d0115b56e8c1b2ec195",
		record.Digest,
	)
	require.NoError(t, record.Validate())
}

func TestConfigurationRecordValidationRejectsInvalidData(t *testing.T) {
	valid := validConfigurationRecord(t)
	pending, err := PendingEncodedValue([]string{"resource.example.id"})
	require.NoError(t, err)
	tests := []struct {
		name    string
		change  func(*ConfigurationRecord)
		message string
	}{
		{
			name:    "library path",
			change:  func(record *ConfigurationRecord) { record.LibraryPath = "" },
			message: "library path is required",
		},
		{
			name:    "schema version",
			change:  func(record *ConfigurationRecord) { record.SchemaVersion = 0 },
			message: "schema version must be greater than zero",
		},
		{
			name:    "schema digest",
			change:  func(record *ConfigurationRecord) { record.SchemaDigest = "bad" },
			message: "schema digest must be a lowercase SHA-256 digest",
		},
		{
			name:    "pending value",
			change:  func(record *ConfigurationRecord) { record.Value = pending },
			message: "configuration value must be concrete",
		},
		{
			name: "unsorted sensitive paths",
			change: func(record *ConfigurationRecord) {
				record.SensitivePaths[0], record.SensitivePaths[1] =
					record.SensitivePaths[1], record.SensitivePaths[0]
			},
			message: "sensitive paths must be unique and sorted",
		},
		{
			name: "duplicate sensitive path",
			change: func(record *ConfigurationRecord) {
				record.SensitivePaths[1] = record.SensitivePaths[0]
			},
			message: "sensitive paths must be unique and sorted",
		},
		{
			name: "pointer without slash",
			change: func(record *ConfigurationRecord) {
				record.SensitivePaths[3] = "servers/1"
			},
			message: "must be empty or start with /",
		},
		{
			name: "invalid pointer escape",
			change: func(record *ConfigurationRecord) {
				record.SensitivePaths[1] = "/labels/a~2b"
			},
			message: "invalid escape",
		},
		{
			name: "unresolved pointer",
			change: func(record *ConfigurationRecord) {
				record.SensitivePaths[1] = "/labels/missing"
			},
			message: "does not resolve",
		},
		{
			name: "noncanonical list index",
			change: func(record *ConfigurationRecord) {
				record.SensitivePaths[3] = "/servers/01"
			},
			message: "noncanonical list index",
		},
		{
			name: "sensitive record outside path",
			change: func(record *ConfigurationRecord) {
				record.SensitiveValues[0].Path = "/endpoint"
			},
			message: "sensitive values do not match sensitive leaves",
		},
		{
			name: "sensitive record on container",
			change: func(record *ConfigurationRecord) {
				record.SensitiveValues[0].Path = "/credentials"
			},
			message: "sensitive values do not match sensitive leaves",
		},
		{
			name: "invalid sensitive ID",
			change: func(record *ConfigurationRecord) {
				record.SensitiveValues[0].ID = "bad"
			},
			message: "sensitive value ID must be 32 lowercase hexadecimal characters",
		},
		{
			name: "duplicate sensitive ID",
			change: func(record *ConfigurationRecord) {
				record.SensitiveValues[1].ID = record.SensitiveValues[0].ID
			},
			message: "sensitive value IDs must be unique",
		},
		{
			name: "unsorted sensitive values",
			change: func(record *ConfigurationRecord) {
				record.SensitiveValues[0], record.SensitiveValues[1] =
					record.SensitiveValues[1], record.SensitiveValues[0]
			},
			message: "sensitive values must be unique and sorted",
		},
		{
			name: "missing sensitive value",
			change: func(record *ConfigurationRecord) {
				record.SensitiveValues = record.SensitiveValues[1:]
			},
			message: "sensitive values do not match sensitive leaves",
		},
		{
			name:    "digest mismatch",
			change:  func(record *ConfigurationRecord) { record.Digest = strings.Repeat("a", 64) },
			message: "configuration digest does not match record contents",
		},
		{
			name:    "invalid digest",
			change:  func(record *ConfigurationRecord) { record.Digest = "bad" },
			message: "digest must be a lowercase SHA-256 digest",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			record := cloneConfigurationRecord(valid)
			tt.change(&record)
			err := record.Validate()
			require.ErrorContains(t, err, tt.message)
		})
	}
}

func TestRootSensitivityUsesCanonicalMapAndListPaths(t *testing.T) {
	definition, err := resolveConfigurationDefinition(
		configurationLibraryPath,
		configurationRegistration(1, nil),
	)
	require.NoError(t, err)
	record, err := definition.newConfigurationRecord(
		"library-config.cloud",
		testConfigurationValue(t),
		[]string{"", "/labels/01"},
		nil,
	)
	require.NoError(t, err)
	paths := make([]string, 0, len(record.SensitiveValues))
	for _, sensitive := range record.SensitiveValues {
		paths = append(paths, sensitive.Path)
	}
	require.Equal(t, []string{
		"/credentials/token",
		"/endpoint",
		"/labels/01",
		"/labels/a~1b~0",
		"/labels/plain",
		"/nullable",
		"/servers/0",
		"/servers/1",
	}, paths)
}

func TestConfigurationDigestCoversCanonicalMetadata(t *testing.T) {
	base := validConfigurationRecord(t)
	definition, err := resolveConfigurationDefinition(
		configurationLibraryPath,
		configurationRegistration(1, nil),
	)
	require.NoError(t, err)

	otherAddress, err := definition.newConfigurationRecordWith(
		"library-config.renamed",
		testConfigurationValue(t),
		base.SensitivePaths,
		nil,
		deterministicIDs(
			strings.Repeat("1", 32),
			strings.Repeat("2", 32),
			strings.Repeat("3", 32),
			strings.Repeat("4", 32),
		),
	)
	require.NoError(t, err)
	require.Equal(t, base.Digest, otherAddress.Digest)

	fields, ok := testConfigurationValue(t).ObjectFields()
	require.True(t, ok)
	fields["endpoint"] = StringValue("https://other.example")
	changedValue, err := ObjectValue(fields)
	require.NoError(t, err)
	changed, err := definition.newConfigurationRecordWith(
		base.Address,
		changedValue,
		base.SensitivePaths,
		&base,
		deterministicIDs(),
	)
	require.NoError(t, err)
	require.NotEqual(t, base.Digest, changed.Digest)

	changedPaths, err := definition.newConfigurationRecordWith(
		base.Address,
		testConfigurationValue(t),
		[]string{"/credentials", "/nullable", "/servers/1"},
		&base,
		deterministicIDs(),
	)
	require.NoError(t, err)
	require.NotEqual(t, base.Digest, changedPaths.Digest)
}

func TestConfigurationRecordReusesOnlyUnchangedSensitiveLeaves(t *testing.T) {
	definition, err := resolveConfigurationDefinition(
		configurationLibraryPath,
		configurationRegistration(1, nil),
	)
	require.NoError(t, err)
	prior, err := definition.newConfigurationRecordWith(
		"library-config.cloud",
		testConfigurationValue(t),
		[]string{"/credentials", "/labels/a~1b~0", "/servers"},
		nil,
		deterministicIDs(
			strings.Repeat("1", 32),
			strings.Repeat("2", 32),
			strings.Repeat("3", 32),
			strings.Repeat("4", 32),
		),
	)
	require.NoError(t, err)

	fields, ok := testConfigurationValue(t).ObjectFields()
	require.True(t, ok)
	credentials, ok := fields["credentials"].ObjectFields()
	require.True(t, ok)
	credentials["token"] = StringValue("changed")
	fields["credentials"], err = ObjectValue(credentials)
	require.NoError(t, err)
	fields["servers"], err = ListValue([]EncodedValue{
		StringValue("second"),
		StringValue("first"),
	})
	require.NoError(t, err)
	updatedValue, err := ObjectValue(fields)
	require.NoError(t, err)

	updated, err := definition.newConfigurationRecordWith(
		prior.Address,
		updatedValue,
		prior.SensitivePaths,
		&prior,
		deterministicIDs(
			strings.Repeat("a", 32),
			strings.Repeat("b", 32),
			strings.Repeat("c", 32),
		),
	)
	require.NoError(t, err)
	require.Equal(t, []SensitiveValueRecord{
		{Path: "/credentials/token", ID: strings.Repeat("a", 32)},
		{Path: "/labels/a~1b~0", ID: strings.Repeat("2", 32)},
		{Path: "/servers/0", ID: strings.Repeat("b", 32)},
		{Path: "/servers/1", ID: strings.Repeat("c", 32)},
	}, updated.SensitiveValues)
	require.NotEqual(t, prior.Digest, updated.Digest)
}

func TestConfigurationRecordRetriesSensitiveIDCollision(t *testing.T) {
	definition, err := resolveConfigurationDefinition(
		configurationLibraryPath,
		configurationRegistration(1, nil),
	)
	require.NoError(t, err)
	calls := 0
	record, err := definition.newConfigurationRecordWith(
		"library-config.cloud",
		testConfigurationValue(t),
		[]string{"/servers"},
		nil,
		func() (string, error) {
			calls++
			switch calls {
			case 1, 2:
				return strings.Repeat("1", 32), nil
			default:
				return strings.Repeat("2", 32), nil
			}
		},
	)
	require.NoError(t, err)
	require.Equal(t, 3, calls)
	require.Equal(t, []SensitiveValueRecord{
		{Path: "/servers/0", ID: strings.Repeat("1", 32)},
		{Path: "/servers/1", ID: strings.Repeat("2", 32)},
	}, record.SensitiveValues)
}

func TestConfigurationRecordAvoidsIDReservedForReusedLeaf(t *testing.T) {
	definition, err := resolveConfigurationDefinition(
		configurationLibraryPath,
		configurationRegistration(1, nil),
	)
	require.NoError(t, err)
	reusedID := strings.Repeat("1", 32)
	prior, err := definition.newConfigurationRecordWith(
		"library-config.cloud",
		testConfigurationValue(t),
		[]string{"/servers/1"},
		nil,
		deterministicIDs(reusedID),
	)
	require.NoError(t, err)

	calls := 0
	newID := strings.Repeat("2", 32)
	record, err := definition.newConfigurationRecordWith(
		prior.Address,
		prior.Value,
		[]string{"/servers"},
		&prior,
		func() (string, error) {
			calls++
			if calls == 1 {
				return reusedID, nil
			}
			return newID, nil
		},
	)
	require.NoError(t, err)
	require.Equal(t, 2, calls)
	require.Equal(t, []SensitiveValueRecord{
		{Path: "/servers/0", ID: newID},
		{Path: "/servers/1", ID: reusedID},
	}, record.SensitiveValues)
}

func TestConfigurationRecordRejectsSensitiveIDGenerationFailure(t *testing.T) {
	definition, err := resolveConfigurationDefinition(
		configurationLibraryPath,
		configurationRegistration(1, nil),
	)
	require.NoError(t, err)

	_, err = definition.newConfigurationRecordWith(
		"library-config.cloud",
		testConfigurationValue(t),
		[]string{"/nullable"},
		nil,
		func() (string, error) { return "", errors.New("random source failed") },
	)
	require.ErrorContains(t, err, "random source failed")

	_, err = definition.newConfigurationRecordWith(
		"library-config.cloud",
		testConfigurationValue(t),
		[]string{"/nullable"},
		nil,
		func() (string, error) { return "bad", nil },
	)
	require.ErrorContains(t, err, "sensitive value ID must be 32 lowercase hexadecimal characters")
}

func TestNoConfigRecordIsCanonical(t *testing.T) {
	definition, err := resolveConfigurationDefinition(configurationLibraryPath, nil)
	require.NoError(t, err)
	empty, err := ObjectValue(nil)
	require.NoError(t, err)
	record, err := definition.newConfigurationRecord("", empty, nil, nil)
	require.NoError(t, err)
	require.Empty(t, record.Address)
	require.Equal(t, 1, record.SchemaVersion)
	require.Equal(t, definition.schemaDigest, record.SchemaDigest)
	require.NotNil(t, record.SensitivePaths)
	require.NotNil(t, record.SensitiveValues)
	require.Empty(t, record.SensitivePaths)
	require.Empty(t, record.SensitiveValues)
	require.NoError(t, record.Validate())

	_, err = definition.newConfigurationRecord("library-config.cloud", empty, nil, nil)
	require.ErrorContains(t, err, "NoConfig address must be empty")
	_, err = definition.newConfigurationRecord("", StringValue("bad"), nil, nil)
	require.ErrorContains(t, err, "NoConfig value must be an empty object")
	_, err = definition.newConfigurationRecord("", empty, []string{""}, nil)
	require.ErrorContains(t, err, "NoConfig must not declare sensitive paths")
}

func TestConfiguredRecordRequiresAddressAndCurrentSchema(t *testing.T) {
	definition, err := resolveConfigurationDefinition(
		configurationLibraryPath,
		configurationRegistration(1, nil),
	)
	require.NoError(t, err)
	_, err = definition.newConfigurationRecord("", testConfigurationValue(t), nil, nil)
	require.ErrorContains(t, err, "configuration address is required")

	fields, err := ObjectValue(map[string]EncodedValue{
		"endpoint": StringValue("incomplete"),
	})
	require.NoError(t, err)
	_, err = definition.newConfigurationRecord("library-config.cloud", fields, nil, nil)
	require.Error(t, err)
}

func TestConfigurationMigrationProducesCurrentRecord(t *testing.T) {
	oldDefinition, err := resolveConfigurationDefinition(
		configurationLibraryPath,
		configurationRegistration(1, nil),
	)
	require.NoError(t, err)
	prior := validConfigurationRecord(t)
	fields, ok := prior.Value.ObjectFields()
	require.True(t, ok)
	fields["endpoint"] = StringValue("https://migrated.example")
	migratedValue, err := ObjectValue(fields)
	require.NoError(t, err)
	require.Equal(t, oldDefinition.schemaDigest, prior.SchemaDigest)

	current, err := resolveConfigurationDefinition(
		configurationLibraryPath,
		configurationRegistration(2, func(
			oldVersion int,
			value EncodedValue,
		) (EncodedValue, error) {
			require.Equal(t, 1, oldVersion)
			require.Equal(t, prior.Value, value)
			return migratedValue, nil
		}),
	)
	require.NoError(t, err)
	record, err := current.migrateConfigurationRecord(prior)
	require.NoError(t, err)
	require.Equal(t, 2, record.SchemaVersion)
	require.Equal(t, current.schemaDigest, record.SchemaDigest)
	require.Equal(t, migratedValue, record.Value)
	require.Equal(t, prior.SensitiveValues, record.SensitiveValues)
	require.NotEqual(t, prior.Digest, record.Digest)
}

func TestConfigurationMigrationAcceptsCurrentRecord(t *testing.T) {
	definition, err := resolveConfigurationDefinition(
		configurationLibraryPath,
		configurationRegistration(1, nil),
	)
	require.NoError(t, err)
	prior := validConfigurationRecord(t)

	record, err := definition.migrateConfigurationRecord(prior)
	require.NoError(t, err)
	require.Equal(t, prior, record)
	require.NotSame(t, &prior.SensitivePaths[0], &record.SensitivePaths[0])
	require.NotSame(t, &prior.SensitiveValues[0], &record.SensitiveValues[0])
}

func TestConfigurationMigrationValidatesCurrentRecord(t *testing.T) {
	definition, err := resolveConfigurationDefinition(
		configurationLibraryPath,
		configurationRegistration(1, nil),
	)
	require.NoError(t, err)
	prior := validConfigurationRecord(t)
	prior.Address = ""
	require.NoError(t, prior.Validate())

	_, err = definition.migrateConfigurationRecord(prior)
	require.ErrorContains(t, err, "configuration address is required")
}

func TestConfigurationMigrationRejectsInvalidTransition(t *testing.T) {
	providerErr := errors.New("cannot migrate configuration")
	valid := validConfigurationRecord(t)
	tests := []struct {
		name         string
		version      int
		migration    cfg.ConfigurationMigrationFunc
		change       func(*ConfigurationRecord)
		message      string
		panicErrorOp string
	}{
		{
			name:    "newer recorded version",
			version: 1,
			change:  func(record *ConfigurationRecord) { record.SchemaVersion = 2 },
			message: "recorded configuration version 2 is newer than registered version 1",
		},
		{
			name:    "missing migration",
			version: 2,
			message: "no configuration migration registered for version 1",
		},
		{
			name:    "migration error",
			version: 2,
			migration: func(int, EncodedValue) (EncodedValue, error) {
				return EncodedValue{}, providerErr
			},
			message: providerErr.Error(),
		},
		{
			name:    "migration panic",
			version: 2,
			migration: func(int, EncodedValue) (EncodedValue, error) {
				panic("bad migration")
			},
			panicErrorOp: "migrating this library configuration",
		},
		{
			name:    "pending migration result",
			version: 2,
			migration: func(int, EncodedValue) (EncodedValue, error) {
				value, err := PendingEncodedValue([]string{"resource.example.id"})
				return value, err
			},
			message: "configuration value must be concrete",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			definition, err := resolveConfigurationDefinition(
				configurationLibraryPath,
				configurationRegistration(tt.version, tt.migration),
			)
			require.NoError(t, err)
			prior := cloneConfigurationRecord(valid)
			if tt.change != nil {
				tt.change(&prior)
			}
			_, err = definition.migrateConfigurationRecord(prior)
			if tt.panicErrorOp != "" {
				var panicErr *PanicError
				require.ErrorAs(t, err, &panicErr)
				require.Equal(t, tt.panicErrorOp, panicErr.Op)
				return
			}
			require.ErrorContains(t, err, tt.message)
		})
	}
}

func TestConfigurationMigrationRejectsChangedSchemaAtCurrentVersion(t *testing.T) {
	type otherConfiguration struct {
		Endpoint string
	}
	current, err := resolveConfigurationDefinition(
		configurationLibraryPath,
		&cfg.ConfigurationType[*otherConfiguration]{
			SchemaVersion: 1,
			New:           func() *otherConfiguration { return &otherConfiguration{} },
		},
	)
	require.NoError(t, err)

	_, err = current.migrateConfigurationRecord(validConfigurationRecord(t))
	require.ErrorContains(t, err, "configuration schema changed without increasing its version")
}

func TestConfigurationMigrationRejectsDifferentLibrary(t *testing.T) {
	definition, err := resolveConfigurationDefinition(
		"example.com/other",
		configurationRegistration(1, nil),
	)
	require.NoError(t, err)

	_, err = definition.migrateConfigurationRecord(validConfigurationRecord(t))
	require.ErrorContains(t, err, "recorded configuration belongs to library")
}

func TestPlannedConfigurationValidation(t *testing.T) {
	record := validConfigurationRecord(t)
	tests := []struct {
		name          string
		configuration PlannedConfiguration
		message       string
	}{
		{
			name: "concrete",
			configuration: PlannedConfiguration{
				Kind:   PlannedConfigurationConcrete,
				Record: &record,
			},
		},
		{
			name: "pending",
			configuration: PlannedConfiguration{
				Kind: PlannedConfigurationPending,
				PendingRefs: []string{
					"action.run.output",
					"data-source.lookup.id",
					"input.region",
					"resource.a.id",
				},
			},
		},
		{
			name:          "unknown kind",
			configuration: PlannedConfiguration{Kind: "other"},
			message:       "unknown planned configuration kind",
		},
		{
			name:          "concrete without record",
			configuration: PlannedConfiguration{Kind: PlannedConfigurationConcrete},
			message:       "concrete planned configuration requires a record",
		},
		{
			name: "concrete with refs",
			configuration: PlannedConfiguration{
				Kind:        PlannedConfigurationConcrete,
				Record:      &record,
				PendingRefs: []string{"resource.a.id"},
			},
			message: "concrete planned configuration forbids pending references",
		},
		{
			name:          "pending without refs",
			configuration: PlannedConfiguration{Kind: PlannedConfigurationPending},
			message:       "pending planned configuration requires references",
		},
		{
			name: "pending with record",
			configuration: PlannedConfiguration{
				Kind:        PlannedConfigurationPending,
				Record:      &record,
				PendingRefs: []string{"resource.a.id"},
			},
			message: "pending planned configuration forbids a record",
		},
		{
			name: "unsorted pending refs",
			configuration: PlannedConfiguration{
				Kind:        PlannedConfigurationPending,
				PendingRefs: []string{"resource.b.id", "resource.a.id"},
			},
			message: "pending references must be unique and sorted",
		},
		{
			name: "duplicate pending refs",
			configuration: PlannedConfiguration{
				Kind:        PlannedConfigurationPending,
				PendingRefs: []string{"resource.a.id", "resource.a.id"},
			},
			message: "pending references must be unique and sorted",
		},
		{
			name: "empty pending ref",
			configuration: PlannedConfiguration{
				Kind:        PlannedConfigurationPending,
				PendingRefs: []string{""},
			},
			message: "pending reference must not be empty",
		},
		{
			name: "invalid pending ref",
			configuration: PlannedConfiguration{
				Kind:        PlannedConfigurationPending,
				PendingRefs: []string{"resource..id"},
			},
			message: "pending reference is invalid",
		},
		{
			name: "invalid pending root",
			configuration: PlannedConfiguration{
				Kind:        PlannedConfigurationPending,
				PendingRefs: []string{"local.region"},
			},
			message: "pending reference is invalid",
		},
		{
			name: "pending root without selection",
			configuration: PlannedConfiguration{
				Kind:        PlannedConfigurationPending,
				PendingRefs: []string{"resource"},
			},
			message: "pending reference is invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.configuration.Validate()
			if tt.message == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.message)
		})
	}
}
