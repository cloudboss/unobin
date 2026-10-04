package libraryapi

import (
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	tests := []struct {
		identifier string
		version    Version
	}{
		{"1.0", Version{Major: 1}},
		{"2.3", Version{Major: 2, Minor: 3}},
		{"18446744073709551615.18446744073709551615", Version{Major: ^uint64(0), Minor: ^uint64(0)}},
	}
	for _, tt := range tests {
		t.Run(tt.identifier, func(t *testing.T) {
			version, err := Parse(tt.identifier)
			require.NoError(t, err)
			assert.Equal(t, tt.version, version)
			assert.Equal(t, tt.identifier, version.String())
		})
	}
}

func TestParseInvalid(t *testing.T) {
	for _, identifier := range []string{
		"", "0.0", "01.0", "1.01", "1", "1.", ".1", "1.0.0", "v1.0", "1.0-rc.1",
		"1.0+build", " 1.0", "1.0 ", "1.0\n", "1.\t0", "+1.0", "1.-1", "a.0", "1.a",
		"١.٠", "18446744073709551616.0", "1.18446744073709551616",
	} {
		t.Run(identifier, func(t *testing.T) {
			version, err := Parse(identifier)
			require.Error(t, err)
			assert.Equal(t, Version{}, version)
			assert.Contains(t, err.Error(), "invalid library API")
		})
	}
}

func TestCheck(t *testing.T) {
	descriptor := Descriptor{
		FormatVersion: 1, ImplementedAPIs: []string{"1.5", "2.3"}, GeneratorAPI: "2.3",
	}
	for _, required := range []string{"1.0", "1.5", "2.0", "2.2", "2.3"} {
		t.Run(required, func(t *testing.T) {
			require.NoError(t, Check(required, descriptor))
		})
	}

	err := Check("2.4", descriptor)
	var newer *NewerMinorError
	require.ErrorAs(t, err, &newer)
	assert.Equal(t, Version{Major: 2, Minor: 4}, newer.Required)
	assert.Equal(t, Version{Major: 2, Minor: 3}, newer.Implemented)
	assert.Contains(t, err.Error(), "2.4")
	assert.Contains(t, err.Error(), "2.3")

	err = Check("3.0", descriptor)
	var unsupported *UnsupportedMajorError
	require.ErrorAs(t, err, &unsupported)
	assert.Equal(t, Version{Major: 3}, unsupported.Required)
	assert.Equal(t, []string{"1.5", "2.3"}, unsupported.ImplementedAPIs)
	assert.Contains(t, err.Error(), "3.0")
	unsupported.ImplementedAPIs[0] = "9.0"
	assert.Equal(t, []string{"1.5", "2.3"}, descriptor.ImplementedAPIs)

	require.Error(t, Check("2.3.0", descriptor))
}

func TestDescriptorValidation(t *testing.T) {
	tests := []struct {
		name       string
		descriptor Descriptor
		message    string
	}{
		{"zero", Descriptor{}, "format version"},
		{"future format", Descriptor{FormatVersion: 2}, "format version"},
		{"empty APIs", Descriptor{FormatVersion: 1}, "nonempty"},
		{
			"invalid API",
			Descriptor{FormatVersion: 1, ImplementedAPIs: []string{"1.0.0"}, GeneratorAPI: "1.0.0"},
			"invalid library API",
		},
		{
			"duplicate major",
			Descriptor{FormatVersion: 1, ImplementedAPIs: []string{"1.0", "1.1"}, GeneratorAPI: "1.1"},
			"increasing major order",
		},
		{
			"unordered majors",
			Descriptor{FormatVersion: 1, ImplementedAPIs: []string{"2.0", "1.0"}, GeneratorAPI: "1.0"},
			"increasing major order",
		},
		{
			"missing generator",
			Descriptor{FormatVersion: 1, ImplementedAPIs: []string{"1.0"}},
			"generator API",
		},
		{
			"earlier generator minor",
			Descriptor{FormatVersion: 1, ImplementedAPIs: []string{"1.1"}, GeneratorAPI: "1.0"},
			"generator API",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.ErrorContains(t, tt.descriptor.Validate(), tt.message)
			// Invalid toolchain data takes precedence over an invalid requirement.
			require.ErrorContains(t, Check("invalid", tt.descriptor), tt.message)
		})
	}
}

func TestReadDescriptor(t *testing.T) {
	moduleFS := fstest.MapFS{
		"pkg/libraryapi/descriptor.json": &fstest.MapFile{Data: descriptorJSON},
	}
	descriptor, err := ReadDescriptor(moduleFS)
	require.NoError(t, err)
	assert.Equal(t, Descriptor{
		FormatVersion: 1, ImplementedAPIs: []string{"1.0"}, GeneratorAPI: "1.0",
	}, descriptor)

	_, err = ReadDescriptor(fstest.MapFS{
		"descriptor.json": &fstest.MapFile{Data: descriptorJSON},
	})
	require.ErrorIs(t, err, fs.ErrNotExist)
	assert.Contains(t, err.Error(), "pkg/libraryapi/descriptor.json")
	_, err = ReadDescriptor(unreadableFS{})
	require.ErrorIs(t, err, fs.ErrPermission)
}

func TestRejectInvalidDescriptorData(t *testing.T) {
	for _, data := range []string{
		``, `null`, `[]`, `{}`, `{"format-version":1`,
		`{"format-version":1,"implemented-apis":["1.0"],"generator-api":"1.0"} {}`,
		`{"format-version":1,"implemented-apis":["1.0"],"generator-api":"1.0","extra":true}`,
		`{"format-version":1,"format-version":1,"implemented-apis":["1.0"],"generator-api":"1.0"}`,
		`{"format-version":1,"implemented-apis":["1.0"],` +
			`"implemented-apis":["1.0"],"generator-api":"1.0"}`,
		`{"format-version":1,"implemented-apis":["1.0"],"generator-api":"1.0","generator-api":"1.0"}`,
		`{"format-version":1,"implemented-apis":["1.0"],"generator-api":"1.0",` +
			`"generator-\u0061pi":"1.0"}`,
		`{"format-version":2,"implemented-apis":["1.0"],"generator-api":"1.0"}`,
		`{"format-version":1,"implemented-apis":[],"generator-api":"1.0"}`,
		`{"format-version":1,"implemented-apis":null,"generator-api":"1.0"}`,
		`{"format-version":1,"implemented-apis":[null],"generator-api":"1.0"}`,
		`{"format-version":1,"implemented-apis":["1.0","1.1"],"generator-api":"1.0"}`,
		`{"format-version":1,"implemented-apis":["1.0"],"generator-api":"1.1"}`,
		`{"format-version":1,"implemented-apis":["01.0"],"generator-api":"01.0"}`,
		`{"format-version":1.0,"implemented-apis":["1.0"],"generator-api":"1.0"}`,
		`{"format-version":"1","implemented-apis":["1.0"],"generator-api":"1.0"}`,
		`{"format-version":1,"implemented-apis":"1.0","generator-api":"1.0"}`,
		`{"format-version":1,"implemented-apis":["1.0"],"generator-api":null}`,
	} {
		t.Run(data, func(t *testing.T) {
			moduleFS := fstest.MapFS{
				"pkg/libraryapi/descriptor.json": &fstest.MapFile{Data: []byte(data)},
			}
			_, err := ReadDescriptor(moduleFS)
			require.Error(t, err)
			assert.Panics(t, func() { mustDescriptor([]byte(data)) })
		})
	}
}

func TestCurrentDescriptor(t *testing.T) {
	embedded := mustDescriptor(descriptorJSON)
	require.NoError(t, embedded.Validate())
	assert.Equal(t, embedded, Current())
	current := Current()
	current.ImplementedAPIs[0] = "9.0"
	current.GeneratorAPI = "9.0"
	assert.Equal(t, embedded, Current())
}

type unreadableFS struct{}

func (unreadableFS) Open(name string) (fs.File, error) {
	return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrPermission}
}
