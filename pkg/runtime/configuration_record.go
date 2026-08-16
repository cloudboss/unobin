package runtime

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"maps"
	"slices"

	internalconfig "github.com/cloudboss/unobin/internal/configuration"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/cloudboss/unobin/pkg/typecheck"
)

type ConfigurationRecord = state.ConfigurationRecord
type SensitiveValueRecord = state.SensitiveValueRecord

type PlannedConfigurationKind string

const (
	PlannedConfigurationConcrete PlannedConfigurationKind = "concrete"
	PlannedConfigurationPending  PlannedConfigurationKind = "pending"
)

type PlannedConfiguration struct {
	Kind        PlannedConfigurationKind `json:"kind"`
	Record      *ConfigurationRecord     `json:"record,omitempty"`
	PendingRefs []string                 `json:"pending-refs,omitempty"`
}

func (c PlannedConfiguration) Validate() error {
	switch c.Kind {
	case PlannedConfigurationConcrete:
		if c.Record == nil {
			return fmt.Errorf("concrete planned configuration requires a record")
		}
		if len(c.PendingRefs) > 0 {
			return fmt.Errorf("concrete planned configuration forbids pending references")
		}
		if err := c.Record.Validate(); err != nil {
			return fmt.Errorf("concrete planned configuration: %w", err)
		}
		return nil
	case PlannedConfigurationPending:
		if c.Record != nil {
			return fmt.Errorf("pending planned configuration forbids a record")
		}
		if len(c.PendingRefs) == 0 {
			return fmt.Errorf("pending planned configuration requires references")
		}
		for i, ref := range c.PendingRefs {
			if ref == "" {
				return fmt.Errorf("pending reference must not be empty")
			}
			if !validPendingReference(ref) {
				return fmt.Errorf("pending reference is invalid: %q", ref)
			}
			if i > 0 && ref <= c.PendingRefs[i-1] {
				return fmt.Errorf("pending references must be unique and sorted")
			}
		}
		return nil
	default:
		return fmt.Errorf("unknown planned configuration kind %q", c.Kind)
	}
}

func validPendingReference(ref string) bool {
	expr, err := lang.ParseExpr("pending reference", []byte(ref))
	if err != nil {
		return false
	}
	path, ok := expr.(*lang.DotPath)
	if !ok || path.Root == nil || len(path.Segments) == 0 {
		return false
	}
	switch path.Root.Name {
	case "input", "resource", "data-source", "action":
		return DotPathString(path) == ref
	default:
		return false
	}
}

type resolvedConfigurationDefinition struct {
	libraryPath   string
	schemaVersion int
	schemaDigest  string
	migrate       cfg.ConfigurationMigrationFunc
	schemaFields  []typecheck.ObjectField
	library       *Library
	noConfig      bool
}

func resolveConfigurationDefinition(
	libraryPath string,
	registration cfg.Registration,
) (resolvedConfigurationDefinition, error) {
	return resolveLibraryConfigurationDefinition(libraryPath, &Library{
		Configuration: registration,
	})
}

func resolveLibraryConfigurationDefinition(
	libraryPath string,
	library *Library,
) (resolvedConfigurationDefinition, error) {
	if libraryPath == "" {
		return resolvedConfigurationDefinition{}, fmt.Errorf(
			"configuration definition: library path is required",
		)
	}
	if library == nil || library.Configuration == nil {
		return resolvedConfigurationDefinition{
			libraryPath:   libraryPath,
			schemaVersion: 1,
			schemaDigest:  cfg.DigestView(nil, nil, nil),
			noConfig:      true,
		}, nil
	}
	registration := library.Configuration
	if registration.SchemaVersionNumber() < 1 {
		return resolvedConfigurationDefinition{}, fmt.Errorf(
			"configuration schema version must be greater than zero",
		)
	}
	if err := cfg.ValidateConfigurationType(registration); err != nil {
		return resolvedConfigurationDefinition{}, fmt.Errorf(
			"configuration definition: %w",
			err,
		)
	}
	schema, ok, err := LibraryConfigSchemaFromLibrary(libraryPath, library)
	if err != nil {
		return resolvedConfigurationDefinition{}, fmt.Errorf(
			"configuration definition: %w",
			err,
		)
	}
	if !ok || !isLowerSHA256(schema.Digest) {
		return resolvedConfigurationDefinition{}, fmt.Errorf(
			"configuration schema is unavailable",
		)
	}
	return resolvedConfigurationDefinition{
		libraryPath:   libraryPath,
		schemaVersion: registration.SchemaVersionNumber(),
		schemaDigest:  schema.Digest,
		migrate:       registration.Migration(),
		schemaFields:  slices.Clone(schema.Fields),
		library:       library,
	}, nil
}

func (d resolvedConfigurationDefinition) newConfigurationRecord(
	address string,
	value EncodedValue,
	sensitivePaths []string,
	prior *ConfigurationRecord,
) (ConfigurationRecord, error) {
	return d.newConfigurationRecordWith(
		address,
		value,
		sensitivePaths,
		prior,
		randomSensitiveValueID,
	)
}

func (d resolvedConfigurationDefinition) newConfigurationRecordWith(
	address string,
	value EncodedValue,
	sensitivePaths []string,
	prior *ConfigurationRecord,
	newID func() (string, error),
) (ConfigurationRecord, error) {
	if err := d.validateConfigurationValue(address, value, sensitivePaths); err != nil {
		return ConfigurationRecord{}, err
	}
	if prior != nil && prior.LibraryPath != d.libraryPath {
		return ConfigurationRecord{}, fmt.Errorf(
			"prior configuration belongs to library %q, not %q",
			prior.LibraryPath,
			d.libraryPath,
		)
	}
	record := ConfigurationRecord{
		Address:        address,
		LibraryPath:    d.libraryPath,
		SchemaVersion:  d.schemaVersion,
		SchemaDigest:   d.schemaDigest,
		Value:          value,
		SensitivePaths: slices.Clone(sensitivePaths),
	}
	return internalconfig.Build(record, prior, newID)
}

func (d resolvedConfigurationDefinition) migrateConfigurationRecord(
	prior ConfigurationRecord,
) (ConfigurationRecord, error) {
	if prior.LibraryPath != d.libraryPath {
		return ConfigurationRecord{}, fmt.Errorf(
			"recorded configuration belongs to library %q, not %q",
			prior.LibraryPath,
			d.libraryPath,
		)
	}
	if prior.SchemaVersion > d.schemaVersion {
		return ConfigurationRecord{}, fmt.Errorf(
			"recorded configuration version %d is newer than registered version %d",
			prior.SchemaVersion,
			d.schemaVersion,
		)
	}
	if err := prior.Validate(); err != nil {
		return ConfigurationRecord{}, fmt.Errorf("recorded configuration: %w", err)
	}
	if prior.SchemaVersion == d.schemaVersion {
		if prior.SchemaDigest != d.schemaDigest {
			return ConfigurationRecord{}, fmt.Errorf(
				"configuration schema changed without increasing its version",
			)
		}
		if err := d.validateConfigurationValue(
			prior.Address,
			prior.Value,
			prior.SensitivePaths,
		); err != nil {
			return ConfigurationRecord{}, fmt.Errorf("recorded configuration: %w", err)
		}
		return internalconfig.Clone(prior), nil
	}
	if d.migrate == nil {
		return ConfigurationRecord{}, fmt.Errorf(
			"no configuration migration registered for version %d",
			prior.SchemaVersion,
		)
	}
	value, err := guard(
		"migrating this library configuration",
		false,
		func() (EncodedValue, error) {
			return d.migrate(prior.SchemaVersion, prior.Value)
		},
	)
	if err != nil {
		return ConfigurationRecord{}, err
	}
	return d.newConfigurationRecord(
		prior.Address,
		value,
		prior.SensitivePaths,
		&prior,
	)
}

func (d resolvedConfigurationDefinition) validateConfigurationValue(
	address string,
	value EncodedValue,
	sensitivePaths []string,
) error {
	if value.HasPending() {
		return fmt.Errorf("configuration value must be concrete")
	}
	fields, ok := value.ObjectFields()
	if d.noConfig {
		if address != "" {
			return fmt.Errorf("NoConfig address must be empty")
		}
		if !ok || len(fields) != 0 {
			return fmt.Errorf("NoConfig value must be an empty object")
		}
		if len(sensitivePaths) != 0 {
			return fmt.Errorf("NoConfig must not declare sensitive paths")
		}
		return nil
	}
	if !ok {
		return fmt.Errorf("configuration value must be an object")
	}
	if address == "" {
		return fmt.Errorf("configuration address is required")
	}
	raw, err := encodedConfigurationObject(fields)
	if err != nil {
		return err
	}
	if err := checkConfigObject(d.schemaFields, raw, nil); err != nil {
		return fmt.Errorf("configuration value: %w", err)
	}
	if _, err := decodeLibraryConfig(d.library, raw); err != nil {
		return fmt.Errorf("configuration value: %w", err)
	}
	return nil
}

func encodedConfigurationObject(
	fields map[string]EncodedValue,
) (map[string]any, error) {
	raw := make(map[string]any, len(fields))
	for _, name := range slices.Sorted(maps.Keys(fields)) {
		value, present, err := encodedConfigurationValue(fields[name])
		if err != nil {
			return nil, fmt.Errorf("configuration field %q: %w", name, err)
		}
		if present {
			raw[name] = value
		}
	}
	return raw, nil
}

func encodedConfigurationValue(value EncodedValue) (any, bool, error) {
	switch value.Kind() {
	case EncodedValueAbsent:
		return nil, false, nil
	case EncodedValueNull:
		return nil, true, nil
	case EncodedValueBoolean:
		result, _ := value.Boolean()
		return result, true, nil
	case EncodedValueString:
		result, _ := value.String()
		return result, true, nil
	case EncodedValueInteger:
		result, _ := value.Integer()
		return result, true, nil
	case EncodedValueNumber:
		result, _ := value.Number()
		return result, true, nil
	case EncodedValueList:
		items, _ := value.Items()
		result := make([]any, len(items))
		for i, item := range items {
			decoded, present, err := encodedConfigurationValue(item)
			if err != nil {
				return nil, false, err
			}
			if !present {
				return nil, false, fmt.Errorf("list element %d is absent", i)
			}
			result[i] = decoded
		}
		return result, true, nil
	case EncodedValueMap:
		entries, _ := value.MapEntries()
		result := make(map[string]any, len(entries))
		for _, key := range slices.Sorted(maps.Keys(entries)) {
			decoded, present, err := encodedConfigurationValue(entries[key])
			if err != nil {
				return nil, false, err
			}
			if !present {
				return nil, false, fmt.Errorf("map value %q is absent", key)
			}
			result[key] = decoded
		}
		return result, true, nil
	case EncodedValueObject:
		fields, _ := value.ObjectFields()
		result, err := encodedConfigurationObject(fields)
		return result, true, err
	case EncodedValuePending:
		return nil, false, fmt.Errorf("configuration value must be concrete")
	default:
		return nil, false, fmt.Errorf("configuration value kind is invalid")
	}
}

func randomSensitiveValueID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}
