package runtime

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cloudboss/unobin/pkg/sdk/state"
)

// IdentityRecord is the persisted resource identity record.
type IdentityRecord = state.IdentityRecord

func (d resolvedResourceDefinition[In, Out, Config]) identityDefinitionDigest(
	libraryPath, export string,
) (string, error) {
	if libraryPath == "" {
		return "", fmt.Errorf("identity definition: library path is required")
	}
	if export == "" {
		return "", fmt.Errorf("identity definition: export is required")
	}

	fields := make([]identityDigestField, len(d.addressInputs))
	for i, field := range d.addressInputs {
		if field.path == "" {
			return "", fmt.Errorf("identity definition: address input %d has no path", i)
		}
		if !isLowerSHA256(field.schemaDigest) {
			return "", fmt.Errorf(
				"identity definition: address input %q has an invalid schema digest",
				field.path,
			)
		}
		fields[i] = identityDigestField{
			Path:         field.path,
			SchemaDigest: field.schemaDigest,
		}
	}
	payload := identityDigestPayload{
		LibraryPath:   libraryPath,
		Export:        export,
		Scope:         d.identityScope,
		Version:       d.identityVersion,
		HasStableID:   d.stableID != nil,
		AddressInputs: fields,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode identity definition: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func (d resolvedResourceDefinition[In, Out, Config]) newIdentityRecord(
	libraryPath, export string,
	inputs In,
	outputs Out,
) (IdentityRecord, error) {
	digest, err := d.identityDefinitionDigest(libraryPath, export)
	if err != nil {
		return IdentityRecord{}, err
	}
	record := IdentityRecord{
		DefinitionDigest: digest,
		Version:          d.identityVersion,
	}
	if d.stableID == nil {
		return record, nil
	}
	stableID, err := guard(
		"deriving this resource's stable ID",
		false,
		func() (string, error) {
			return d.stableID(inputs, outputs)
		},
	)
	if err != nil {
		return IdentityRecord{}, err
	}
	if stableID == "" {
		return IdentityRecord{}, fmt.Errorf("stable ID must not be empty")
	}
	record.StableID = copyString(&stableID)
	return record, nil
}

func (d resolvedResourceDefinition[In, Out, Config]) migrateIdentityRecord(
	libraryPath, export string,
	resource ResourceMigrationState,
	prior IdentityRecord,
) (IdentityRecord, error) {
	if err := prior.Validate(); err != nil {
		return IdentityRecord{}, fmt.Errorf("recorded identity: %w", err)
	}
	digest, err := d.identityDefinitionDigest(libraryPath, export)
	if err != nil {
		return IdentityRecord{}, err
	}
	switch {
	case prior.Version > d.identityVersion:
		return IdentityRecord{}, fmt.Errorf(
			"recorded identity version %d is newer than registered version %d",
			prior.Version,
			d.identityVersion,
		)
	case prior.Version == d.identityVersion:
		return d.acceptCurrentIdentityRecord(digest, prior)
	case d.identityMigrate == nil:
		return IdentityRecord{}, fmt.Errorf(
			"no identity migration registered for version %d",
			prior.Version,
		)
	}

	stableID, err := guard(
		"migrating this resource's identity",
		false,
		func() (*string, error) {
			return d.identityMigrate(prior.Version, resource, copyString(prior.StableID))
		},
	)
	if err != nil {
		return IdentityRecord{}, err
	}
	if d.stableID == nil && stableID != nil {
		return IdentityRecord{}, fmt.Errorf(
			"identity migration must not return a stable ID",
		)
	}
	if d.stableID != nil && stableID == nil {
		return IdentityRecord{}, fmt.Errorf("identity migration must return a stable ID")
	}
	if stableID != nil && *stableID == "" {
		return IdentityRecord{}, fmt.Errorf("identity migration returned an empty stable ID")
	}
	record := IdentityRecord{
		DefinitionDigest: digest,
		Version:          d.identityVersion,
		StableID:         copyString(stableID),
	}
	if err := record.Validate(); err != nil {
		return IdentityRecord{}, fmt.Errorf("migrated identity: %w", err)
	}
	return record, nil
}

func (d resolvedResourceDefinition[In, Out, Config]) validateObservedIdentity(
	libraryPath, export string,
	inputs In,
	outputs Out,
	prior IdentityRecord,
) (IdentityRecord, error) {
	expected, err := d.currentIdentityRecord(libraryPath, export, prior)
	if err != nil {
		return IdentityRecord{}, err
	}
	observed, err := d.newIdentityRecord(libraryPath, export, inputs, outputs)
	if err != nil {
		return IdentityRecord{}, err
	}
	if !sameString(expected.StableID, observed.StableID) {
		return IdentityRecord{}, fmt.Errorf(
			"recorded stable ID %q does not match observed stable ID %q",
			stringValue(expected.StableID),
			stringValue(observed.StableID),
		)
	}
	return observed, nil
}

func (d resolvedResourceDefinition[In, Out, Config]) validateUpdatedIdentity(
	libraryPath, export string,
	inputs In,
	outputs Out,
	prior IdentityRecord,
) (IdentityRecord, error) {
	expected, err := d.currentIdentityRecord(libraryPath, export, prior)
	if err != nil {
		return IdentityRecord{}, err
	}
	updated, err := d.newIdentityRecord(libraryPath, export, inputs, outputs)
	if err != nil {
		return IdentityRecord{}, err
	}
	if !sameString(expected.StableID, updated.StableID) {
		return IdentityRecord{}, fmt.Errorf(
			"prior stable ID %q does not match updated stable ID %q",
			stringValue(expected.StableID),
			stringValue(updated.StableID),
		)
	}
	return expected, nil
}

type identityDigestPayload struct {
	LibraryPath   string                `json:"library-path"`
	Export        string                `json:"export"`
	Scope         IdentityScope         `json:"scope"`
	Version       int                   `json:"version"`
	HasStableID   bool                  `json:"has-stable-id"`
	AddressInputs []identityDigestField `json:"address-inputs"`
}

type identityDigestField struct {
	Path         string `json:"path"`
	SchemaDigest string `json:"schema-digest"`
}

func (d resolvedResourceDefinition[In, Out, Config]) currentIdentityRecord(
	libraryPath, export string,
	record IdentityRecord,
) (IdentityRecord, error) {
	if err := record.Validate(); err != nil {
		return IdentityRecord{}, fmt.Errorf("recorded identity: %w", err)
	}
	digest, err := d.identityDefinitionDigest(libraryPath, export)
	if err != nil {
		return IdentityRecord{}, err
	}
	if record.Version < d.identityVersion {
		return IdentityRecord{}, fmt.Errorf(
			"recorded identity version %d requires migration to version %d",
			record.Version,
			d.identityVersion,
		)
	}
	if record.Version > d.identityVersion {
		return IdentityRecord{}, fmt.Errorf(
			"recorded identity version %d is newer than registered version %d",
			record.Version,
			d.identityVersion,
		)
	}
	return d.acceptCurrentIdentityRecord(digest, record)
}

func (d resolvedResourceDefinition[In, Out, Config]) acceptCurrentIdentityRecord(
	digest string,
	record IdentityRecord,
) (IdentityRecord, error) {
	if record.DefinitionDigest != digest {
		return IdentityRecord{}, fmt.Errorf(
			"identity definition changed without increasing its version",
		)
	}
	if d.stableID == nil && record.StableID != nil {
		return IdentityRecord{}, fmt.Errorf(
			"identity record has a stable ID but the definition does not declare one",
		)
	}
	if d.stableID != nil && record.StableID == nil {
		return IdentityRecord{}, fmt.Errorf(
			"identity record is missing the stable ID declared by the definition",
		)
	}
	record.StableID = copyString(record.StableID)
	return record, nil
}

func isLowerSHA256(value string) bool {
	if len(value) != sha256.Size*2 || strings.ToLower(value) != value {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

func copyString(value *string) *string {
	if value == nil {
		return nil
	}
	result := *value
	return &result
}

func sameString(a, b *string) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
