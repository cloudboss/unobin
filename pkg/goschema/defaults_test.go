package goschema

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/lang"
)

const defaultsLibrary = `package lib

import (
	"time"

	"github.com/cloudboss/unobin/pkg/defaults"
	"github.com/cloudboss/unobin/pkg/runtime"
)

func Library() *runtime.Library {
	return &runtime.Library{
		Name: "lib",
		Resources: map[string]runtime.ResourceRegistration{
			"thing": runtime.MakeResource[Thing, *ThingOutput, any](),
		},
	}
}

type Thing struct {
	Mode    int64
	Method  string
	On      bool
	Ratio   float64
	Timeout time.Duration
	Dir     string
	Code    Code
	Ptr     *string
	Items   []Item
}

type Code struct {
	Retries int64
}

type Item struct {
	A *string
}

type ThingOutput struct {
	ID string
}
`

func TestReadExtractsDefaults(t *testing.T) {
	tests := []struct {
		name      string
		method    string
		extra     string
		wantSpecs []lang.DefaultSpec
		wantWarns []string
	}{
		{
			name: "integer value",
			method: `func (f Thing) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.Value(f.Mode, 420),
	}
}`,
			wantSpecs: []lang.DefaultSpec{{Field: "input.mode", Value: "420"}},
		},
		{
			name: "octal literal normalizes to decimal",
			method: `func (f Thing) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.Value(f.Mode, 0o644),
	}
}`,
			wantSpecs: []lang.DefaultSpec{{Field: "input.mode", Value: "420"}},
		},
		{
			name: "string value",
			method: `func (f Thing) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.Value(f.Method, "GET"),
	}
}`,
			wantSpecs: []lang.DefaultSpec{{Field: "input.method", Value: "'GET'"}},
		},
		{
			name: "boolean value",
			method: `func (f Thing) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.Value(f.On, true),
	}
}`,
			wantSpecs: []lang.DefaultSpec{{Field: "input.on", Value: "true"}},
		},
		{
			name: "float value",
			method: `func (f Thing) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.Value(f.Ratio, 0.5),
	}
}`,
			wantSpecs: []lang.DefaultSpec{{Field: "input.ratio", Value: "0.5"}},
		},
		{
			name: "negative value",
			method: `func (f Thing) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.Value(f.Mode, -1),
	}
}`,
			wantSpecs: []lang.DefaultSpec{{Field: "input.mode", Value: "-1"}},
		},
		{
			name: "duration constant folds to nanoseconds",
			method: `func (f Thing) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.Value(f.Timeout, time.Second),
	}
}`,
			wantSpecs: []lang.DefaultSpec{{Field: "input.timeout", Value: "1000000000"}},
		},
		{
			name: "duration product folds",
			method: `func (f Thing) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.Value(f.Timeout, 5*time.Minute),
	}
}`,
			wantSpecs: []lang.DefaultSpec{{Field: "input.timeout", Value: "300000000000"}},
		},
		{
			name: "duration product folds with the constant first",
			method: `func (f Thing) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.Value(f.Timeout, time.Second*30),
	}
}`,
			wantSpecs: []lang.DefaultSpec{{Field: "input.timeout", Value: "30000000000"}},
		},
		{
			name: "optional constructor warns",
			method: `func (f Thing) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.Optional(f.Dir),
	}
}`,
			wantWarns: []string{`Thing: unsupported default constructor "Optional"`},
		},
		{
			name: "allow absent constructor warns",
			method: `func (f Thing) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.AllowAbsent(f.Dir),
	}
}`,
			wantWarns: []string{`Thing: unsupported default constructor "AllowAbsent"`},
		},
		{
			name: "nested field by dotted path",
			method: `func (f Thing) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.Value(f.Code.Retries, 3),
	}
}`,
			wantSpecs: []lang.DefaultSpec{{Field: "input.code.retries", Value: "3"}},
		},
		{
			name: "declaration order is preserved",
			method: `func (f Thing) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.Value(f.Method, "GET"),
		defaults.Value(f.Dir, "tmp"),
		defaults.Value(f.Mode, 420),
	}
}`,
			wantSpecs: []lang.DefaultSpec{
				{Field: "input.method", Value: "'GET'"},
				{Field: "input.dir", Value: "'tmp'"},
				{Field: "input.mode", Value: "420"},
			},
		},
		{
			name: "unsupported constructor warns",
			method: `func (f Thing) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.Whatever(f.Mode),
	}
}`,
			wantWarns: []string{`Thing: unsupported default constructor "Whatever"`},
		},
		{
			name: "non-literal value warns",
			method: `func (f Thing) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.Value(f.Mode, modeDefault),
	}
}`,
			extra:     `const modeDefault = 420`,
			wantWarns: []string{"Thing: a default must be a literal, got modeDefault"},
		},
		{
			name: "body that is not a single return warns",
			method: `func (f Thing) Defaults() []defaults.Default {
	out := []defaults.Default{
		defaults.Value(f.Mode, 420),
	}
	return out
}`,
			wantWarns: []string{
				"Thing: the Defaults method must be a single return of a default list",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := defaultsLibrary + "\n" + tt.method + "\n"
			if tt.extra != "" {
				src += "\n" + tt.extra + "\n"
			}
			schema, warnings, err := readConstraintLibrary(t, src)
			require.NoError(t, err)
			require.Equal(t, tt.wantWarns, warnings)
			require.Equal(t, tt.wantSpecs, schema.Resources["thing"].Defaults)
		})
	}
}

func TestReadRejectsMalformedDefaults(t *testing.T) {
	tests := []struct {
		name    string
		method  string
		wantErr string
	}{
		{
			name: "duplicate field",
			method: `func (f Thing) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.Value(f.Mode, 420),
		defaults.Value(f.Mode, 600),
	}
}`,
			wantErr: `duplicate default for "mode"`,
		},
		{
			name: "value on a pointer field",
			method: `func (f Thing) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.Value(f.Ptr, "x"),
	}
}`,
			wantErr: `pointer field "ptr" cannot take a default`,
		},
		{
			name: "indexed list element",
			method: `func (f Thing) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.Value(f.Items[0].A, "x"),
	}
}`,
			wantErr: "a default cannot index a list element",
		},
		{
			name: "unknown field",
			method: `func (f Thing) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.Value(f.Bogus, 1),
	}
}`,
			wantErr: `"Bogus"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := defaultsLibrary + "\n" + tt.method + "\n"
			_, _, err := readConstraintLibrary(t, src)
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestReadExtractsDefaultsBesideConstraints(t *testing.T) {
	src := `package lib

import (
	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/defaults"
	"github.com/cloudboss/unobin/pkg/runtime"
)

func Library() *runtime.Library {
	return &runtime.Library{
		Name: "lib",
		Resources: map[string]runtime.ResourceRegistration{
			"thing": runtime.MakeResource[Thing, *ThingOutput, any](),
		},
	}
}

type Thing struct {
	Method string
	Dir    string
}

type ThingOutput struct {
	ID string
}

func (f Thing) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.RequiredWith(f.Method, f.Dir),
	}
}

func (f Thing) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.Value(f.Method, "GET"),
	}
}
`
	schema, warnings, err := readConstraintLibrary(t, src)
	require.NoError(t, err)
	require.Empty(t, warnings)
	thing := schema.Resources["thing"]
	require.Equal(t, []lang.ConstraintSpec{
		{Kind: "required-with", Fields: []string{"input.method", "input.dir"}},
	}, thing.Constraints)
	require.Equal(t, []lang.DefaultSpec{
		{Field: "input.method", Value: "'GET'"},
	}, thing.Defaults)
}
