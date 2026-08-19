package state

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	internalconfig "github.com/cloudboss/unobin/internal/configuration"
	encodedvalue "github.com/cloudboss/unobin/pkg/encoding/value"
)

func v2Object(t *testing.T, fields map[string]encodedvalue.Value) encodedvalue.Value {
	t.Helper()
	value, err := encodedvalue.Object(fields)
	require.NoError(t, err)
	return value
}

func v2Configuration(t *testing.T) ConfigurationRecord {
	t.Helper()
	record, err := internalconfig.Build(internalconfig.Record{
		Address:       "library-config.cloud",
		LibraryPath:   "example.com/cloud",
		SchemaVersion: 1,
		SchemaDigest:  strings.Repeat("a", 64),
		Value: v2Object(t, map[string]encodedvalue.Value{
			"region": encodedvalue.String("us-east-1"),
		}),
		SensitivePaths: []string{},
	}, nil, nil)
	require.NoError(t, err)
	return record
}

func validV2Binding() CanonicalBinding {
	return CanonicalBinding{
		LibraryPath: "example.com/cloud",
		Export:      "server",
	}
}

func validV2ResourceTarget(t *testing.T) ResourceTarget {
	t.Helper()
	stableID := "server-123"
	return ResourceTarget{
		Binding:       validV2Binding(),
		SchemaVersion: 2,
		Inputs: v2Object(t, map[string]encodedvalue.Value{
			"name":   encodedvalue.String("api"),
			"secret": encodedvalue.String("input-secret"),
		}),
		Outputs: v2Object(t, map[string]encodedvalue.Value{
			"id": encodedvalue.String("server-123"),
		}),
		Configuration: v2Configuration(t),
		Identity: IdentityRecord{
			DefinitionDigest: strings.Repeat("b", 64),
			Version:          1,
			StableID:         &stableID,
		},
		DependsOn:            []string{"resource.network"},
		SensitiveInputPaths:  []string{"/secret"},
		SensitiveOutputPaths: []string{"/id"},
	}
}

func validV2ActionPayload(t *testing.T) ActionStatePayload {
	t.Helper()
	return ActionStatePayload{
		Binding: CanonicalBinding{LibraryPath: "example.com/cloud", Export: "notify"},
		Inputs: v2Object(t, map[string]encodedvalue.Value{
			"message": encodedvalue.String("ok"),
		}),
		Outputs: v2Object(t, map[string]encodedvalue.Value{
			"sent": encodedvalue.Boolean(true),
		}),
		Configuration:        v2Configuration(t),
		TriggerHash:          "trigger-1",
		DependsOn:            []string{"resource.api"},
		SensitiveInputPaths:  []string{},
		SensitiveOutputPaths: []string{},
	}
}

func validV2DataSourcePayload(t *testing.T) DataSourceStatePayload {
	t.Helper()
	return DataSourceStatePayload{
		Binding: CanonicalBinding{LibraryPath: "example.com/cloud", Export: "image"},
		Inputs: v2Object(t, map[string]encodedvalue.Value{
			"name": encodedvalue.String("base"),
		}),
		Outputs: v2Object(t, map[string]encodedvalue.Value{
			"id": encodedvalue.String("ami-1"),
		}),
		Configuration:        v2Configuration(t),
		DependsOn:            []string{},
		SensitiveInputPaths:  []string{},
		SensitiveOutputPaths: []string{},
	}
}

func validV2CompositePayload(t *testing.T) CompositeStatePayload {
	t.Helper()
	return CompositeStatePayload{
		Category: "resource",
		Binding: CanonicalBinding{
			LibraryPath: "example.com/composites",
			Export:      "application",
		},
		Inputs: v2Object(t, map[string]encodedvalue.Value{
			"name": encodedvalue.String("api"),
		}),
		Outputs: v2Object(t, map[string]encodedvalue.Value{
			"url": encodedvalue.String("https://example.com"),
		}),
		DependsOn:            []string{},
		SensitiveInputPaths:  []string{},
		SensitiveOutputPaths: []string{},
	}
}

func TestCanonicalBindingValidation(t *testing.T) {
	tests := []struct {
		name    string
		binding CanonicalBinding
		message string
	}{
		{name: "valid", binding: validV2Binding()},
		{
			name: "uppercase export",
			binding: CanonicalBinding{
				LibraryPath: "example.com/cloud",
				Export:      "ServerV2",
			},
		},
		{
			name:    "missing library path",
			binding: CanonicalBinding{Export: "server"},
			message: "library path is required",
		},
		{
			name:    "missing export",
			binding: CanonicalBinding{LibraryPath: "example.com/cloud"},
			message: "export is required",
		},
		{
			name: "invalid export",
			binding: CanonicalBinding{
				LibraryPath: "example.com/cloud",
				Export:      "not valid",
			},
			message: "export is invalid",
		},
		{
			name: "trailing hyphen",
			binding: CanonicalBinding{
				LibraryPath: "example.com/cloud",
				Export:      "server-",
			},
			message: "export is invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.binding.Validate()
			if tt.message == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.message)
		})
	}
}

func TestResourceTargetValidation(t *testing.T) {
	pending, err := encodedvalue.Pending([]string{"resource.network.id"})
	require.NoError(t, err)
	tests := []struct {
		name    string
		change  func(*ResourceTarget)
		message string
	}{
		{name: "valid"},
		{
			name:    "binding",
			change:  func(target *ResourceTarget) { target.Binding.Export = "" },
			message: "binding: export is required",
		},
		{
			name:    "schema version",
			change:  func(target *ResourceTarget) { target.SchemaVersion = 0 },
			message: "schema version must be greater than zero",
		},
		{
			name: "configuration library",
			change: func(target *ResourceTarget) {
				target.Binding.LibraryPath = "example.com/other"
			},
			message: "configuration library path does not match binding",
		},
		{
			name: "pending input",
			change: func(target *ResourceTarget) {
				target.Inputs = v2Object(t, map[string]encodedvalue.Value{"name": pending})
			},
			message: "inputs must be concrete",
		},
		{
			name:    "input root",
			change:  func(target *ResourceTarget) { target.Inputs = encodedvalue.String("bad") },
			message: "inputs must be an object",
		},
		{
			name: "pending output",
			change: func(target *ResourceTarget) {
				target.Outputs = v2Object(t, map[string]encodedvalue.Value{"id": pending})
			},
			message: "outputs must be concrete",
		},
		{
			name:    "output root",
			change:  func(target *ResourceTarget) { target.Outputs = encodedvalue.String("bad") },
			message: "outputs must be an object",
		},
		{
			name: "configuration",
			change: func(target *ResourceTarget) {
				target.Configuration.Digest = strings.Repeat("c", 64)
			},
			message: "configuration digest does not match",
		},
		{
			name:    "identity",
			change:  func(target *ResourceTarget) { target.Identity.Version = 0 },
			message: "identity version must be greater than zero",
		},
		{
			name: "unsorted dependencies",
			change: func(target *ResourceTarget) {
				target.DependsOn = []string{"resource.z", "resource.a"}
			},
			message: "dependencies must be unique and sorted",
		},
		{
			name:    "invalid dependency",
			change:  func(target *ResourceTarget) { target.DependsOn = []string{"output.value"} },
			message: "dependency is invalid",
		},
		{
			name: "unsorted input paths",
			change: func(target *ResourceTarget) {
				target.SensitiveInputPaths = []string{"/secret", "/name"}
			},
			message: "sensitive input paths must be unique and sorted",
		},
		{
			name: "missing input path",
			change: func(target *ResourceTarget) {
				target.SensitiveInputPaths = []string{"/missing"}
			},
			message: "sensitive input path \"/missing\": does not resolve",
		},
		{
			name: "missing output path",
			change: func(target *ResourceTarget) {
				target.SensitiveOutputPaths = []string{"/missing"}
			},
			message: "sensitive output path \"/missing\": does not resolve",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target := validV2ResourceTarget(t)
			if tt.change != nil {
				tt.change(&target)
			}
			err := target.Validate()
			if tt.message == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.message)
		})
	}
}

func TestNonResourceStatePayloadValidation(t *testing.T) {
	t.Run("action", func(t *testing.T) {
		payload := validV2ActionPayload(t)
		require.NoError(t, payload.Validate())
		payload.Inputs = encodedvalue.String("bad")
		require.ErrorContains(t, payload.Validate(), "inputs must be an object")
	})

	t.Run("data source", func(t *testing.T) {
		payload := validV2DataSourcePayload(t)
		require.NoError(t, payload.Validate())
		payload.Outputs = encodedvalue.String("bad")
		require.ErrorContains(t, payload.Validate(), "outputs must be an object")
	})

	t.Run("composite", func(t *testing.T) {
		payload := validV2CompositePayload(t)
		require.NoError(t, payload.Validate())
		payload.Category = "output"
		require.ErrorContains(t, payload.Validate(), "category is invalid")
	})
}

func TestStatePayloadValidation(t *testing.T) {
	resource := ResourceStatePayload{Target: validV2ResourceTarget(t)}
	action := validV2ActionPayload(t)
	dataSource := validV2DataSourcePayload(t)
	composite := validV2CompositePayload(t)
	tests := []struct {
		name    string
		kind    StateEntryKind
		payload StatePayload
		message string
	}{
		{
			name: "resource",
			kind: StateResource,
			payload: StatePayload{
				Kind:     StateResource,
				Resource: &resource,
			},
		},
		{
			name: "action",
			kind: StateAction,
			payload: StatePayload{
				Kind:   StateAction,
				Action: &action,
			},
		},
		{
			name: "data source",
			kind: StateDataSource,
			payload: StatePayload{
				Kind:       StateDataSource,
				DataSource: &dataSource,
			},
		},
		{
			name: "composite",
			kind: StateComposite,
			payload: StatePayload{
				Kind:      StateComposite,
				Composite: &composite,
			},
		},
		{
			name: "kind mismatch",
			kind: StateAction,
			payload: StatePayload{
				Kind:     StateResource,
				Resource: &resource,
			},
			message: "payload kind resource does not match entry kind action",
		},
		{
			name:    "unknown kind",
			kind:    "other",
			payload: StatePayload{Kind: "other"},
			message: "unknown state entry kind",
		},
		{
			name:    "missing selected payload",
			kind:    StateResource,
			payload: StatePayload{Kind: StateResource},
			message: "resource payload is required",
		},
		{
			name: "extra payload",
			kind: StateResource,
			payload: StatePayload{
				Kind:     StateResource,
				Resource: &resource,
				Action:   &action,
			},
			message: "resource payload forbids other payloads",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.payload.Validate(tt.kind)
			if tt.message == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.message)
		})
	}
}

func validSnapshotV2(t *testing.T) SnapshotV2 {
	t.Helper()
	action := validV2ActionPayload(t)
	resource := ResourceStatePayload{Target: validV2ResourceTarget(t)}
	return SnapshotV2{
		FormatVersion: SnapshotFormatVersionV2,
		Factory: FactoryInfo{
			Name:            "deploy",
			Version:         "v1.0.0",
			ContentRevision: "revision-1",
		},
		Stack:       "production",
		GeneratedAt: time.Date(2026, 8, 16, 4, 0, 0, 0, time.UTC),
		Entries: []StateEntryV2{
			{
				Address: "action.notify",
				Kind:    StateAction,
				Payload: StatePayload{Kind: StateAction, Action: &action},
			},
			{
				Address: "resource.api",
				Kind:    StateResource,
				Payload: StatePayload{Kind: StateResource, Resource: &resource},
			},
		},
		Outputs: v2Object(t, map[string]encodedvalue.Value{
			"url": encodedvalue.String("https://example.com"),
		}),
		SensitivePaths: []string{"/url"},
	}
}

func TestNewSnapshotV2InitializesValidSnapshot(t *testing.T) {
	before := time.Now().UTC()
	factory := FactoryInfo{
		Name:            "deploy",
		Version:         "v1.0.0",
		ContentRevision: "revision-1",
	}
	snapshot, err := NewSnapshotV2(factory, "production")
	after := time.Now().UTC()

	require.NoError(t, err)
	require.Equal(t, SnapshotFormatVersionV2, snapshot.FormatVersion)
	require.Equal(t, factory, snapshot.Factory)
	require.Equal(t, "production", snapshot.Stack)
	require.False(t, snapshot.GeneratedAt.Before(before))
	require.False(t, snapshot.GeneratedAt.After(after))
	require.NotNil(t, snapshot.Entries)
	require.Empty(t, snapshot.Entries)
	outputs, ok := snapshot.Outputs.ObjectFields()
	require.True(t, ok)
	require.Empty(t, outputs)
	require.NotNil(t, snapshot.SensitivePaths)
	require.Empty(t, snapshot.SensitivePaths)
	require.NoError(t, snapshot.Validate())
}

func TestNewSnapshotV2RejectsInvalidMetadata(t *testing.T) {
	tests := []struct {
		name    string
		factory FactoryInfo
		stack   string
		message string
	}{
		{
			name:    "missing factory name",
			factory: FactoryInfo{Version: "v1.0.0", ContentRevision: "revision-1"},
			stack:   "production",
			message: "factory name is required",
		},
		{
			name: "missing factory version",
			factory: FactoryInfo{
				Name:            "deploy",
				ContentRevision: "revision-1",
			},
			stack:   "production",
			message: "factory version is required",
		},
		{
			name: "missing factory content revision",
			factory: FactoryInfo{
				Name:    "deploy",
				Version: "v1.0.0",
			},
			stack:   "production",
			message: "factory content revision is required",
		},
		{
			name: "missing stack",
			factory: FactoryInfo{
				Name:            "deploy",
				Version:         "v1.0.0",
				ContentRevision: "revision-1",
			},
			message: "stack is required",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			snapshot, err := NewSnapshotV2(test.factory, test.stack)
			require.ErrorContains(t, err, test.message)
			require.Nil(t, snapshot)
		})
	}
}

func TestSnapshotV2Find(t *testing.T) {
	snapshot := validSnapshotV2(t)

	entry := snapshot.Find("resource.api")
	require.NotNil(t, entry)
	require.Equal(t, snapshot.Entries[1], *entry)
	require.Nil(t, snapshot.Find("resource.missing"))

	var missing *SnapshotV2
	require.Nil(t, missing.Find("resource.api"))
}

func TestSnapshotV2SetEntry(t *testing.T) {
	snapshot := validSnapshotV2(t)
	dataSource := validV2DataSourcePayload(t)
	entry := StateEntryV2{
		Address: "data-source.image",
		Kind:    StateDataSource,
		Payload: StatePayload{
			Kind:       StateDataSource,
			DataSource: &dataSource,
		},
	}

	require.NoError(t, snapshot.SetEntry(entry))
	require.Equal(t, []string{
		"action.notify",
		"data-source.image",
		"resource.api",
	}, snapshotEntryAddresses(snapshot))
	require.Equal(t, entry, *snapshot.Find(entry.Address))
	require.NoError(t, snapshot.Validate())

	entry.Payload.DataSource.DependsOn = []string{"resource.network"}
	require.Empty(t, snapshot.Find(entry.Address).Payload.DataSource.DependsOn)

	action := validV2ActionPayload(t)
	action.TriggerHash = "trigger-2"
	replacement := StateEntryV2{
		Address: "action.notify",
		Kind:    StateAction,
		Payload: StatePayload{Kind: StateAction, Action: &action},
	}
	require.NoError(t, snapshot.SetEntry(replacement))
	require.Equal(t, replacement, *snapshot.Find(replacement.Address))
	require.Equal(t, []string{
		"action.notify",
		"data-source.image",
		"resource.api",
	}, snapshotEntryAddresses(snapshot))

	empty := validSnapshotV2(t)
	empty.Entries = []StateEntryV2{}
	require.NoError(t, empty.SetEntry(entry))
	require.Equal(t, []string{"data-source.image"}, snapshotEntryAddresses(empty))
}

func TestSnapshotV2SetEntryRejectsInvalidMutation(t *testing.T) {
	snapshot := validSnapshotV2(t)
	before, err := encodeSnapshotV2(snapshot)
	require.NoError(t, err)
	resource := ResourceStatePayload{Target: validV2ResourceTarget(t)}

	err = snapshot.SetEntry(StateEntryV2{
		Address: "action.api",
		Kind:    StateResource,
		Payload: StatePayload{Kind: StateResource, Resource: &resource},
	})
	require.ErrorContains(t, err, "address category action does not match resource")
	after, encodeErr := encodeSnapshotV2(snapshot)
	require.NoError(t, encodeErr)
	require.Equal(t, before, after)

	var missing *SnapshotV2
	err = missing.SetEntry(StateEntryV2{})
	require.ErrorContains(t, err, "snapshot is required")
}

func TestSnapshotV2RemoveEntry(t *testing.T) {
	snapshot := validSnapshotV2(t)

	require.NoError(t, snapshot.RemoveEntry("action.notify"))
	require.Equal(t, []string{"resource.api"}, snapshotEntryAddresses(snapshot))
	require.Nil(t, snapshot.Find("action.notify"))
	require.NoError(t, snapshot.Validate())

	require.NoError(t, snapshot.RemoveEntry("resource.missing"))
	require.Equal(t, []string{"resource.api"}, snapshotEntryAddresses(snapshot))
	require.NoError(t, snapshot.RemoveEntry("resource.api"))
	require.NotNil(t, snapshot.Entries)
	require.Empty(t, snapshot.Entries)
	require.NoError(t, snapshot.Validate())
}

func TestSnapshotV2RemoveEntryRejectsInvalidMutation(t *testing.T) {
	snapshot := validSnapshotV2(t)
	before, err := encodeSnapshotV2(snapshot)
	require.NoError(t, err)

	err = snapshot.RemoveEntry("not-an-address")
	require.ErrorContains(t, err, "entry address is invalid")
	after, encodeErr := encodeSnapshotV2(snapshot)
	require.NoError(t, encodeErr)
	require.Equal(t, before, after)

	var missing *SnapshotV2
	err = missing.RemoveEntry("resource.api")
	require.ErrorContains(t, err, "snapshot is required")
}

func snapshotEntryAddresses(snapshot SnapshotV2) []string {
	addresses := make([]string, len(snapshot.Entries))
	for i := range snapshot.Entries {
		addresses[i] = snapshot.Entries[i].Address
	}
	return addresses
}

func TestStateEntryV2Validation(t *testing.T) {
	resource := ResourceStatePayload{Target: validV2ResourceTarget(t)}
	entry := StateEntryV2{
		Address: "resource.api",
		Kind:    StateResource,
		Payload: StatePayload{Kind: StateResource, Resource: &resource},
	}
	require.NoError(t, entry.Validate())

	entry.Address = "action.api"
	require.ErrorContains(t, entry.Validate(), "address category action does not match resource")

	entry.Kind = "other"
	entry.Payload.Kind = "other"
	require.ErrorContains(t, entry.Validate(), "unknown state entry kind")
}

func TestSnapshotV2Validation(t *testing.T) {
	pending, err := encodedvalue.Pending([]string{"resource.api.id"})
	require.NoError(t, err)
	tests := []struct {
		name    string
		change  func(*SnapshotV2)
		message string
	}{
		{name: "valid"},
		{
			name:    "format version",
			change:  func(snapshot *SnapshotV2) { snapshot.FormatVersion = 1 },
			message: "format version must be 2",
		},
		{
			name:    "factory name",
			change:  func(snapshot *SnapshotV2) { snapshot.Factory.Name = "" },
			message: "factory name is required",
		},
		{
			name:    "factory version",
			change:  func(snapshot *SnapshotV2) { snapshot.Factory.Version = "" },
			message: "factory version is required",
		},
		{
			name: "factory content revision",
			change: func(snapshot *SnapshotV2) {
				snapshot.Factory.ContentRevision = ""
			},
			message: "factory content revision is required",
		},
		{
			name:    "stack",
			change:  func(snapshot *SnapshotV2) { snapshot.Stack = "" },
			message: "stack is required",
		},
		{
			name:    "generated time",
			change:  func(snapshot *SnapshotV2) { snapshot.GeneratedAt = time.Time{} },
			message: "generated time is required",
		},
		{
			name: "unsorted entries",
			change: func(snapshot *SnapshotV2) {
				snapshot.Entries[0], snapshot.Entries[1] = snapshot.Entries[1], snapshot.Entries[0]
			},
			message: "entries must have unique addresses sorted",
		},
		{
			name: "duplicate entries",
			change: func(snapshot *SnapshotV2) {
				snapshot.Entries[1].Address = snapshot.Entries[0].Address
			},
			message: "entries must have unique addresses sorted",
		},
		{
			name: "invalid entry",
			change: func(snapshot *SnapshotV2) {
				snapshot.Entries[1].Payload.Kind = StateAction
			},
			message: "entries[1]",
		},
		{
			name: "pending outputs",
			change: func(snapshot *SnapshotV2) {
				snapshot.Outputs = v2Object(t, map[string]encodedvalue.Value{"url": pending})
			},
			message: "outputs must be concrete",
		},
		{
			name:    "output root",
			change:  func(snapshot *SnapshotV2) { snapshot.Outputs = encodedvalue.String("bad") },
			message: "outputs must be an object",
		},
		{
			name: "missing sensitive output",
			change: func(snapshot *SnapshotV2) {
				snapshot.SensitivePaths = []string{"/missing"}
			},
			message: "sensitive path \"/missing\": does not resolve",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshot := validSnapshotV2(t)
			if tt.change != nil {
				tt.change(&snapshot)
			}
			err := snapshot.Validate()
			if tt.message == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.message)
		})
	}
}

func TestSnapshotV2UsesCanonicalBindingFields(t *testing.T) {
	encoded, err := json.Marshal(validSnapshotV2(t))
	require.NoError(t, err)
	require.Contains(
		t,
		string(encoded),
		`"binding":{"library-path":"example.com/cloud","export":"notify"}`,
	)
	require.NotContains(t, string(encoded), `"alias"`)
}
