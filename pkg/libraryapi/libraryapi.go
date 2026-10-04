package libraryapi

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"slices"
	"strconv"
	"strings"
)

type Version struct {
	Major uint64
	Minor uint64
}

type Descriptor struct {
	FormatVersion   int      `json:"format-version"`
	ImplementedAPIs []string `json:"implemented-apis"`
	GeneratorAPI    string   `json:"generator-api"`
}

type UnsupportedMajorError struct {
	Required        Version
	ImplementedAPIs []string
}

func (e *UnsupportedMajorError) Error() string {
	return fmt.Sprintf("required library API %s has an unsupported major; implemented APIs: %s",
		e.Required, strings.Join(e.ImplementedAPIs, ", "))
}

type NewerMinorError struct {
	Required    Version
	Implemented Version
}

func (e *NewerMinorError) Error() string {
	return fmt.Sprintf("required library API %s is newer than implemented API %s",
		e.Required, e.Implemented)
}

//go:embed descriptor.json
var descriptorJSON []byte

var currentDescriptor = mustDescriptor(descriptorJSON)

func Parse(identifier string) (Version, error) {
	majorText, minorText, ok := strings.Cut(identifier, ".")
	if !ok || !decimalComponent(majorText) || !decimalComponent(minorText) || majorText == "0" {
		return Version{}, fmt.Errorf("invalid library API %q: expected major.minor with a positive "+
			"major and no leading zeroes", identifier)
	}
	major, err := strconv.ParseUint(majorText, 10, 64)
	if err != nil {
		return Version{}, fmt.Errorf("invalid library API %q: %w", identifier, err)
	}
	minor, err := strconv.ParseUint(minorText, 10, 64)
	if err != nil {
		return Version{}, fmt.Errorf("invalid library API %q: %w", identifier, err)
	}
	return Version{Major: major, Minor: minor}, nil
}

func decimalComponent(component string) bool {
	if component == "" || len(component) > 1 && component[0] == '0' {
		return false
	}
	for i := range len(component) {
		if component[i] < '0' || component[i] > '9' {
			return false
		}
	}
	return true
}

func (v Version) String() string {
	return fmt.Sprintf("%d.%d", v.Major, v.Minor)
}

func (d Descriptor) Validate() error {
	if d.FormatVersion != 1 {
		return fmt.Errorf("unsupported library API descriptor format version %d", d.FormatVersion)
	}
	if len(d.ImplementedAPIs) == 0 {
		return fmt.Errorf("implemented APIs must be nonempty")
	}
	var priorMajor uint64
	for _, identifier := range d.ImplementedAPIs {
		version, err := Parse(identifier)
		if err != nil {
			return fmt.Errorf("implemented APIs: %w", err)
		}
		if version.Major <= priorMajor {
			return fmt.Errorf("implemented APIs must have unique entries in increasing major order")
		}
		priorMajor = version.Major
	}
	if !slices.Contains(d.ImplementedAPIs, d.GeneratorAPI) {
		return fmt.Errorf("generator API %q must equal an implemented API entry", d.GeneratorAPI)
	}
	return nil
}

func Check(required string, descriptor Descriptor) error {
	if err := descriptor.Validate(); err != nil {
		return err
	}
	version, err := Parse(required)
	if err != nil {
		return err
	}
	for _, identifier := range descriptor.ImplementedAPIs {
		implemented, err := Parse(identifier)
		if err != nil {
			return err
		}
		if implemented.Major != version.Major {
			continue
		}
		if version.Minor > implemented.Minor {
			return &NewerMinorError{Required: version, Implemented: implemented}
		}
		return nil
	}
	return &UnsupportedMajorError{
		Required: version, ImplementedAPIs: slices.Clone(descriptor.ImplementedAPIs),
	}
}

func Current() Descriptor {
	descriptor := currentDescriptor
	descriptor.ImplementedAPIs = slices.Clone(descriptor.ImplementedAPIs)
	return descriptor
}

func ReadDescriptor(moduleFS fs.FS) (Descriptor, error) {
	const path = "pkg/libraryapi/descriptor.json"
	data, err := fs.ReadFile(moduleFS, path)
	if err != nil {
		return Descriptor{}, fmt.Errorf("read library API descriptor %s: %w", path, err)
	}
	descriptor, err := decodeDescriptor(data)
	if err != nil {
		return Descriptor{}, fmt.Errorf("read library API descriptor %s: %w", path, err)
	}
	return descriptor, nil
}

func decodeDescriptor(data []byte) (Descriptor, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	start, err := decoder.Token()
	if err != nil {
		return Descriptor{}, err
	}
	if start != json.Delim('{') {
		return Descriptor{}, fmt.Errorf("library API descriptor must be a JSON object")
	}
	var descriptor Descriptor
	seen := map[string]bool{}
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return Descriptor{}, err
		}
		name, ok := key.(string)
		if !ok {
			return Descriptor{}, fmt.Errorf("library API descriptor field must be a string")
		}
		if seen[name] {
			return Descriptor{}, fmt.Errorf("duplicate library API descriptor field %q", name)
		}
		seen[name] = true
		var target any
		switch name {
		case "format-version":
			target = &descriptor.FormatVersion
		case "implemented-apis":
			target = &descriptor.ImplementedAPIs
		case "generator-api":
			target = &descriptor.GeneratorAPI
		default:
			return Descriptor{}, fmt.Errorf("unknown library API descriptor field %q", name)
		}
		if err := decoder.Decode(target); err != nil {
			return Descriptor{}, fmt.Errorf("library API descriptor field %q: %w", name, err)
		}
	}
	if _, err := decoder.Token(); err != nil {
		return Descriptor{}, err
	}
	if _, err := decoder.Token(); err != io.EOF {
		if err != nil {
			return Descriptor{}, err
		}
		return Descriptor{}, fmt.Errorf("unexpected data after library API descriptor")
	}
	if err := descriptor.Validate(); err != nil {
		return Descriptor{}, err
	}
	return descriptor, nil
}

func mustDescriptor(data []byte) Descriptor {
	descriptor, err := decodeDescriptor(data)
	if err != nil {
		panic(fmt.Errorf("embedded library API descriptor: %w", err))
	}
	return descriptor
}
