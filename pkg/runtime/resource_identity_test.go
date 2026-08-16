package runtime

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	identityLibraryPath = "example.com/cloud/storage"
	identityExport      = "bucket"
)

func TestIdentityDefinitionDigestIsStable(t *testing.T) {
	definition := validRuleDefinition()
	resolved, err := resolveResourceDefinition(definition)
	require.NoError(t, err)

	digest, err := resolved.identityDefinitionDigest(identityLibraryPath, identityExport)
	require.NoError(t, err)
	require.Equal(t, "83c5f70c323999df5160552ee693e52e133613deeb83d6f2df0fa4710adf1078", digest)

	again, err := resolveResourceDefinition(validRuleDefinition())
	require.NoError(t, err)
	againDigest, err := again.identityDefinitionDigest(identityLibraryPath, identityExport)
	require.NoError(t, err)
	require.Equal(t, digest, againDigest)

	definition.SchemaVersion++
	changedSchema, err := resolveResourceDefinition(definition)
	require.NoError(t, err)
	changedSchemaDigest, err := changedSchema.identityDefinitionDigest(
		identityLibraryPath,
		identityExport,
	)
	require.NoError(t, err)
	require.Equal(t, digest, changedSchemaDigest)
}

func TestIdentityDefinitionDigestCoversIdentityMetadata(t *testing.T) {
	base, err := resolveResourceDefinition(validRuleDefinition())
	require.NoError(t, err)
	baseDigest, err := base.identityDefinitionDigest(identityLibraryPath, identityExport)
	require.NoError(t, err)

	zones := InputField(func(value *ruleInput) *[]string { return &value.Zones })
	tests := []struct {
		name        string
		libraryPath string
		export      string
		change      func(*ruleDefinition)
	}{
		{
			name:        "library path",
			libraryPath: "example.com/cloud/other",
			export:      identityExport,
		},
		{
			name:        "export",
			libraryPath: identityLibraryPath,
			export:      "object",
		},
		{
			name:        "scope",
			libraryPath: identityLibraryPath,
			export:      identityExport,
			change: func(definition *ruleDefinition) {
				definition.Identity.Scope = IdentityGlobal
			},
		},
		{
			name:        "version",
			libraryPath: identityLibraryPath,
			export:      identityExport,
			change: func(definition *ruleDefinition) {
				definition.Identity.Version++
			},
		},
		{
			name:        "stable ID declaration",
			libraryPath: identityLibraryPath,
			export:      identityExport,
			change: func(definition *ruleDefinition) {
				definition.Identity.StableID = nil
				definition.Replacement.Drift = nil
			},
		},
		{
			name:        "address inputs",
			libraryPath: identityLibraryPath,
			export:      identityExport,
			change: func(definition *ruleDefinition) {
				definition.Identity.AddressInputs = append(
					definition.Identity.AddressInputs,
					zones,
				)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			definition := validRuleDefinition()
			if tt.change != nil {
				tt.change(&definition)
			}
			resolved, err := resolveResourceDefinition(definition)
			require.NoError(t, err)
			digest, err := resolved.identityDefinitionDigest(tt.libraryPath, tt.export)
			require.NoError(t, err)
			require.NotEqual(t, baseDigest, digest)
		})
	}
}

func TestIdentityDefinitionDigestRequiresCanonicalBinding(t *testing.T) {
	resolved, err := resolveResourceDefinition(validRuleDefinition())
	require.NoError(t, err)

	_, err = resolved.identityDefinitionDigest("", identityExport)
	require.ErrorContains(t, err, "library path is required")

	_, err = resolved.identityDefinitionDigest(identityLibraryPath, "")
	require.ErrorContains(t, err, "export is required")
}

func TestIdentityDefinitionDigestPreservesAddressInputOrder(t *testing.T) {
	name, _, _, _, _, _ := ruleInputFields()
	zones := InputField(func(value *ruleInput) *[]string { return &value.Zones })
	definition := validRuleDefinition()
	definition.Identity.AddressInputs = []AnyInputField[ruleInput]{name, zones}
	first, err := resolveResourceDefinition(definition)
	require.NoError(t, err)
	firstDigest, err := first.identityDefinitionDigest(identityLibraryPath, identityExport)
	require.NoError(t, err)

	definition.Identity.AddressInputs = []AnyInputField[ruleInput]{zones, name}
	second, err := resolveResourceDefinition(definition)
	require.NoError(t, err)
	secondDigest, err := second.identityDefinitionDigest(identityLibraryPath, identityExport)
	require.NoError(t, err)
	require.NotEqual(t, firstDigest, secondDigest)
}

func TestIdentityRecordValidation(t *testing.T) {
	validDigest := strings.Repeat("a", 64)
	tests := []struct {
		name    string
		record  IdentityRecord
		message string
	}{
		{
			name:   "logical address",
			record: IdentityRecord{DefinitionDigest: validDigest, Version: 1},
		},
		{
			name: "stable ID",
			record: IdentityRecord{
				DefinitionDigest: validDigest,
				Version:          1,
				StableID:         new("object-1"),
			},
		},
		{
			name: "short digest",
			record: IdentityRecord{
				DefinitionDigest: strings.Repeat("a", 63),
				Version:          1,
			},
			message: "definition digest must be a lowercase SHA-256 digest",
		},
		{
			name: "uppercase digest",
			record: IdentityRecord{
				DefinitionDigest: strings.Repeat("A", 64),
				Version:          1,
			},
			message: "definition digest must be a lowercase SHA-256 digest",
		},
		{
			name: "invalid digest",
			record: IdentityRecord{
				DefinitionDigest: strings.Repeat("z", 64),
				Version:          1,
			},
			message: "definition digest must be a lowercase SHA-256 digest",
		},
		{
			name: "version",
			record: IdentityRecord{
				DefinitionDigest: validDigest,
			},
			message: "identity version must be greater than zero",
		},
		{
			name: "empty stable ID",
			record: IdentityRecord{
				DefinitionDigest: validDigest,
				Version:          1,
				StableID:         new(""),
			},
			message: "stable ID must not be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.record.Validate()
			if tt.message == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.message)
		})
	}
}

func TestIdentityMigrationUsesMigratedResourceValues(t *testing.T) {
	inputs := StringValue("migrated inputs")
	outputs := StringValue("migrated outputs")
	priorStableID := new("object-v1")
	definition := validRuleDefinition()
	definition.Identity.Version = 2
	definition.Identity.Migrate = func(
		oldVersion int,
		resource ResourceMigrationState,
		stableID *string,
	) (*string, error) {
		require.Equal(t, 1, oldVersion)
		require.Equal(t, inputs, resource.Inputs)
		require.Equal(t, outputs, resource.Outputs)
		require.Equal(t, "object-v1", *stableID)
		*stableID = "changed by callback"
		return new("object-v2"), nil
	}
	resolved, err := resolveResourceDefinition(definition)
	require.NoError(t, err)

	got, err := resolved.migrateIdentityRecord(
		identityLibraryPath,
		identityExport,
		ResourceMigrationState{Inputs: inputs, Outputs: outputs},
		IdentityRecord{
			DefinitionDigest: strings.Repeat("a", 64),
			Version:          1,
			StableID:         priorStableID,
		},
	)
	require.NoError(t, err)
	require.Equal(t, "object-v1", *priorStableID)
	require.Equal(t, 2, got.Version)
	require.Equal(t, "object-v2", *got.StableID)
	expectedDigest, err := resolved.identityDefinitionDigest(identityLibraryPath, identityExport)
	require.NoError(t, err)
	require.Equal(t, expectedDigest, got.DefinitionDigest)
}

func TestIdentityMigrationAcceptsCurrentRecord(t *testing.T) {
	resolved, err := resolveResourceDefinition(validRuleDefinition())
	require.NoError(t, err)
	prior, err := resolved.newIdentityRecord(
		identityLibraryPath,
		identityExport,
		ruleInput{Name: "bucket"},
		&ruleOutput{ID: "object-1"},
	)
	require.NoError(t, err)

	got, err := resolved.migrateIdentityRecord(
		identityLibraryPath,
		identityExport,
		ResourceMigrationState{},
		prior,
	)
	require.NoError(t, err)
	require.Equal(t, prior, got)
	require.NotSame(t, prior.StableID, got.StableID)
}

func TestCurrentIdentityRequiresMatchingStableIDDeclaration(t *testing.T) {
	stable, err := resolveResourceDefinition(validRuleDefinition())
	require.NoError(t, err)
	stableDigest, err := stable.identityDefinitionDigest(identityLibraryPath, identityExport)
	require.NoError(t, err)
	_, err = stable.migrateIdentityRecord(
		identityLibraryPath,
		identityExport,
		ResourceMigrationState{},
		IdentityRecord{DefinitionDigest: stableDigest, Version: 1},
	)
	require.ErrorContains(t, err, "identity record is missing the stable ID")

	definition := validRuleDefinition()
	definition.Identity.StableID = nil
	definition.Replacement.Drift = nil
	logical, err := resolveResourceDefinition(definition)
	require.NoError(t, err)
	logicalDigest, err := logical.identityDefinitionDigest(identityLibraryPath, identityExport)
	require.NoError(t, err)
	_, err = logical.migrateIdentityRecord(
		identityLibraryPath,
		identityExport,
		ResourceMigrationState{},
		IdentityRecord{
			DefinitionDigest: logicalDigest,
			Version:          1,
			StableID:         new("unexpected"),
		},
	)
	require.ErrorContains(t, err, "identity record has a stable ID")
}

func TestIdentityMigrationRejectsInvalidTransition(t *testing.T) {
	validDigest := strings.Repeat("a", 64)
	migrationErr := errors.New("cannot migrate identity")
	tests := []struct {
		name    string
		change  func(*ruleDefinition)
		prior   IdentityRecord
		message string
	}{
		{
			name: "newer recorded version",
			prior: IdentityRecord{
				DefinitionDigest: validDigest,
				Version:          3,
				StableID:         new("object-1"),
			},
			message: "recorded identity version 3 is newer than registered version 2",
		},
		{
			name: "changed definition at current version",
			prior: IdentityRecord{
				DefinitionDigest: validDigest,
				Version:          2,
				StableID:         new("object-1"),
			},
			message: "identity definition changed without increasing its version",
		},
		{
			name: "missing migration",
			prior: IdentityRecord{
				DefinitionDigest: validDigest,
				Version:          1,
				StableID:         new("object-1"),
			},
			message: "no identity migration registered for version 1",
		},
		{
			name: "migration error",
			change: func(definition *ruleDefinition) {
				definition.Identity.Migrate = func(
					int,
					ResourceMigrationState,
					*string,
				) (*string, error) {
					return nil, migrationErr
				}
			},
			prior: IdentityRecord{
				DefinitionDigest: validDigest,
				Version:          1,
				StableID:         new("object-1"),
			},
			message: migrationErr.Error(),
		},
		{
			name: "missing migrated stable ID",
			change: func(definition *ruleDefinition) {
				definition.Identity.Migrate = func(
					int,
					ResourceMigrationState,
					*string,
				) (*string, error) {
					return nil, nil
				}
			},
			prior: IdentityRecord{
				DefinitionDigest: validDigest,
				Version:          1,
				StableID:         new("object-1"),
			},
			message: "identity migration must return a stable ID",
		},
		{
			name: "empty migrated stable ID",
			change: func(definition *ruleDefinition) {
				definition.Identity.Migrate = func(
					int,
					ResourceMigrationState,
					*string,
				) (*string, error) {
					return new(""), nil
				}
			},
			prior: IdentityRecord{
				DefinitionDigest: validDigest,
				Version:          1,
				StableID:         new("object-1"),
			},
			message: "identity migration returned an empty stable ID",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			definition := validRuleDefinition()
			definition.Identity.Version = 2
			if tt.change != nil {
				tt.change(&definition)
			}
			resolved, err := resolveResourceDefinition(definition)
			require.NoError(t, err)
			_, err = resolved.migrateIdentityRecord(
				identityLibraryPath,
				identityExport,
				ResourceMigrationState{},
				tt.prior,
			)
			require.ErrorContains(t, err, tt.message)
		})
	}
}

func TestIdentityMigrationRejectsStableIDForLogicalAddress(t *testing.T) {
	definition := validRuleDefinition()
	definition.Identity.Version = 2
	definition.Identity.StableID = nil
	definition.Replacement.Drift = nil
	definition.Identity.Migrate = func(
		int,
		ResourceMigrationState,
		*string,
	) (*string, error) {
		return new("unexpected"), nil
	}
	resolved, err := resolveResourceDefinition(definition)
	require.NoError(t, err)

	_, err = resolved.migrateIdentityRecord(
		identityLibraryPath,
		identityExport,
		ResourceMigrationState{},
		IdentityRecord{
			DefinitionDigest: strings.Repeat("a", 64),
			Version:          1,
			StableID:         new("legacy"),
		},
	)
	require.ErrorContains(t, err, "identity migration must not return a stable ID")
}

func TestIdentityMigrationToLogicalAddress(t *testing.T) {
	definition := validRuleDefinition()
	definition.Identity.Version = 2
	definition.Identity.StableID = nil
	definition.Replacement.Drift = nil
	definition.Identity.Migrate = func(
		int,
		ResourceMigrationState,
		*string,
	) (*string, error) {
		return nil, nil
	}
	resolved, err := resolveResourceDefinition(definition)
	require.NoError(t, err)

	record, err := resolved.migrateIdentityRecord(
		identityLibraryPath,
		identityExport,
		ResourceMigrationState{},
		IdentityRecord{
			DefinitionDigest: strings.Repeat("a", 64),
			Version:          1,
			StableID:         new("legacy"),
		},
	)
	require.NoError(t, err)
	require.Equal(t, 2, record.Version)
	require.Nil(t, record.StableID)
}

func TestIdentityMigrationReturnsPanicError(t *testing.T) {
	definition := validRuleDefinition()
	definition.Identity.Version = 2
	definition.Identity.Migrate = func(
		int,
		ResourceMigrationState,
		*string,
	) (*string, error) {
		panic("bad migration")
	}
	resolved, err := resolveResourceDefinition(definition)
	require.NoError(t, err)

	_, err = resolved.migrateIdentityRecord(
		identityLibraryPath,
		identityExport,
		ResourceMigrationState{},
		IdentityRecord{
			DefinitionDigest: strings.Repeat("a", 64),
			Version:          1,
			StableID:         new("legacy"),
		},
	)
	var panicErr *PanicError
	require.ErrorAs(t, err, &panicErr)
	require.Equal(t, "migrating this resource's identity", panicErr.Op)
}

func TestNewIdentityRecord(t *testing.T) {
	resolved, err := resolveResourceDefinition(validRuleDefinition())
	require.NoError(t, err)

	record, err := resolved.newIdentityRecord(
		identityLibraryPath,
		identityExport,
		ruleInput{Name: "bucket"},
		&ruleOutput{ID: "object-1"},
	)
	require.NoError(t, err)
	require.Equal(t, 1, record.Version)
	require.Equal(t, "object-1", *record.StableID)
	require.NoError(t, record.Validate())

	definition := validRuleDefinition()
	definition.Identity.StableID = nil
	definition.Replacement.Drift = nil
	logical, err := resolveResourceDefinition(definition)
	require.NoError(t, err)
	logicalRecord, err := logical.newIdentityRecord(
		identityLibraryPath,
		identityExport,
		ruleInput{Name: "bucket"},
		&ruleOutput{},
	)
	require.NoError(t, err)
	require.Nil(t, logicalRecord.StableID)
}

func TestNewIdentityRecordRejectsInvalidStableID(t *testing.T) {
	providerErr := errors.New("identity unavailable")
	tests := []struct {
		name     string
		stableID func(ruleInput, *ruleOutput) (string, error)
		message  string
	}{
		{
			name: "empty",
			stableID: func(ruleInput, *ruleOutput) (string, error) {
				return "", nil
			},
			message: "stable ID must not be empty",
		},
		{
			name: "error",
			stableID: func(ruleInput, *ruleOutput) (string, error) {
				return "", providerErr
			},
			message: providerErr.Error(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			definition := validRuleDefinition()
			definition.Identity.StableID = tt.stableID
			resolved, err := resolveResourceDefinition(definition)
			require.NoError(t, err)
			_, err = resolved.newIdentityRecord(
				identityLibraryPath,
				identityExport,
				ruleInput{Name: "bucket"},
				&ruleOutput{},
			)
			require.ErrorContains(t, err, tt.message)
		})
	}
}

func TestNewIdentityRecordReturnsPanicError(t *testing.T) {
	definition := validRuleDefinition()
	definition.Identity.StableID = func(ruleInput, *ruleOutput) (string, error) {
		panic("bad stable ID")
	}
	resolved, err := resolveResourceDefinition(definition)
	require.NoError(t, err)

	_, err = resolved.newIdentityRecord(
		identityLibraryPath,
		identityExport,
		ruleInput{},
		&ruleOutput{},
	)
	var panicErr *PanicError
	require.ErrorAs(t, err, &panicErr)
	require.Equal(t, "deriving this resource's stable ID", panicErr.Op)
}

func TestObservedIdentityMatchesPriorRecord(t *testing.T) {
	resolved, err := resolveResourceDefinition(validRuleDefinition())
	require.NoError(t, err)
	prior, err := resolved.newIdentityRecord(
		identityLibraryPath,
		identityExport,
		ruleInput{Name: "bucket"},
		&ruleOutput{ID: "object-1"},
	)
	require.NoError(t, err)

	observed, err := resolved.validateObservedIdentity(
		identityLibraryPath,
		identityExport,
		ruleInput{Name: "bucket"},
		&ruleOutput{ID: "object-1"},
		prior,
	)
	require.NoError(t, err)
	require.Equal(t, prior, observed)

	_, err = resolved.validateObservedIdentity(
		identityLibraryPath,
		identityExport,
		ruleInput{Name: "bucket"},
		&ruleOutput{ID: "object-2"},
		prior,
	)
	require.ErrorContains(t, err, `recorded stable ID "object-1"`)
	require.ErrorContains(t, err, `observed stable ID "object-2"`)
}

func TestUpdatedIdentityPreservesPriorRecord(t *testing.T) {
	resolved, err := resolveResourceDefinition(validRuleDefinition())
	require.NoError(t, err)
	prior, err := resolved.newIdentityRecord(
		identityLibraryPath,
		identityExport,
		ruleInput{Name: "bucket"},
		&ruleOutput{ID: "object-1"},
	)
	require.NoError(t, err)

	updated, err := resolved.validateUpdatedIdentity(
		identityLibraryPath,
		identityExport,
		ruleInput{Name: "bucket"},
		&ruleOutput{ID: "object-1"},
		prior,
	)
	require.NoError(t, err)
	require.Equal(t, prior, updated)
	require.NotSame(t, prior.StableID, updated.StableID)

	_, err = resolved.validateUpdatedIdentity(
		identityLibraryPath,
		identityExport,
		ruleInput{Name: "bucket"},
		&ruleOutput{ID: "object-2"},
		prior,
	)
	require.ErrorContains(t, err, `prior stable ID "object-1"`)
	require.ErrorContains(t, err, `updated stable ID "object-2"`)
}

func TestIdentityValidationRejectsChangedDefinition(t *testing.T) {
	resolved, err := resolveResourceDefinition(validRuleDefinition())
	require.NoError(t, err)
	prior := IdentityRecord{
		DefinitionDigest: strings.Repeat("a", 64),
		Version:          1,
		StableID:         new("object-1"),
	}

	_, err = resolved.validateObservedIdentity(
		identityLibraryPath,
		identityExport,
		ruleInput{Name: "bucket"},
		&ruleOutput{ID: "object-1"},
		prior,
	)
	require.ErrorContains(t, err, "identity definition changed without increasing its version")

	_, err = resolved.validateUpdatedIdentity(
		identityLibraryPath,
		identityExport,
		ruleInput{Name: "bucket"},
		&ruleOutput{ID: "object-1"},
		prior,
	)
	require.ErrorContains(t, err, "identity definition changed without increasing its version")
}
