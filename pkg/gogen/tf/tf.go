package tf

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/cloudboss/unobin/pkg/gogen"
)

// Fetcher abstracts terraform CLI calls for testability.
type Fetcher interface {
	FetchSchema(ctx context.Context, source, localName, version string) ([]byte, error)
}

// CLIFetcher runs terraform in a temp directory.
type CLIFetcher struct{}

func (CLIFetcher) FetchSchema(
	ctx context.Context,
	source, localName, version string,
) ([]byte, error) {
	dir, err := os.MkdirTemp("", ".unobin-tf-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	versionConstraint := ""
	if version != "" {
		versionConstraint = fmt.Sprintf("\n      version = \"%s\"", version)
	}
	versions := fmt.Sprintf(`
terraform {
  required_providers {
    %s = {
      source  = "%s"%s
    }
  }
}
`, localName, source, versionConstraint)
	if err := os.WriteFile(dir+"/versions.tf", []byte(versions), 0644); err != nil {
		return nil, err
	}

	init := exec.CommandContext(ctx, "terraform", "init")
	init.Dir = dir
	if out, err := init.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("terraform init: %w\n%s", err, out)
	}

	schema := exec.CommandContext(ctx, "terraform", "providers", "schema", "-json")
	schema.Dir = dir
	out, err := schema.Output()
	if err != nil {
		return nil, fmt.Errorf("terraform providers schema -json: %w", err)
	}
	return out, nil
}

// Adapter implements gogen.SchemaAdapter backed by TF provider schemas.
// provider is the fully-qualified TF registry source (e.g. "hashicorp/aws"
// or "ansible/ansible"). The part after the final slash becomes the local
// provider name for the terraform block and the Go package name.
type Adapter struct {
	Fetcher   Fetcher
	source    string
	localName string
	version   string
}

// NewAdapter creates an Adapter for the given fully-qualified provider source.
// version is an optional TF version constraint (e.g. "~> 5.0").
func NewAdapter(fetcher Fetcher, provider, version string) *Adapter {
	idx := strings.LastIndex(provider, "/")
	return &Adapter{
		Fetcher:   fetcher,
		source:    provider,
		localName: provider[idx+1:],
		version:   version,
	}
}

func (a *Adapter) Name() string { return a.localName }

func (a *Adapter) FetchResources(
	ctx context.Context,
	resources []string,
) ([]gogen.ResourceSchema, error) {
	data, err := a.Fetcher.FetchSchema(ctx, a.source, a.localName, a.version)
	if err != nil {
		return nil, err
	}

	parsed, err := parseProviderSchema(data)
	if err != nil {
		return nil, fmt.Errorf("parse provider schema: %w", err)
	}

	var providerName string
	for k := range parsed.ProviderSchemas {
		providerName = k
		break
	}
	ps := parsed.ProviderSchemas[providerName]

	prefixes := resourcePrefixes(a.localName, resources)

	var all []gogen.ResourceSchema
	for name, rs := range ps.ResourceSchemas {
		if !matchesPrefix(name, prefixes) {
			continue
		}
		schema, err := convertResource(name, rs)
		if err != nil {
			return nil, fmt.Errorf("convert %s: %w", name, err)
		}
		all = append(all, schema)
	}

	return all, nil
}

// FetchConfiguration extracts the TF provider's own configuration block
// (e.g. aws { region = ... }) and returns it as a generic schema for
// the renderer. A nil result means the provider declares no
// configuration attributes; the renderer then skips emitting a
// ProviderConfig struct and library Configuration entry.
func (a *Adapter) FetchConfiguration(ctx context.Context) (*gogen.ConfigurationSchema, error) {
	data, err := a.Fetcher.FetchSchema(ctx, a.source, a.localName, a.version)
	if err != nil {
		return nil, err
	}

	parsed, err := parseProviderSchema(data)
	if err != nil {
		return nil, fmt.Errorf("parse provider schema: %w", err)
	}

	var providerName string
	for k := range parsed.ProviderSchemas {
		providerName = k
		break
	}
	ps := parsed.ProviderSchemas[providerName]
	if ps.Provider == nil || len(ps.Provider.Block.Attributes) == 0 {
		return nil, nil
	}

	var fields []gogen.Field
	for attrName, attr := range ps.Provider.Block.Attributes {
		goField := tfAttrNameToGo(attrName)
		if goField == "" {
			continue
		}
		if attr.Computed && !attr.Optional && !attr.Required {
			continue
		}
		fields = append(fields, gogen.Field{
			Name:        goField,
			GoType:      tfTypeToGo(attr.Type),
			Description: attr.Description,
			Required:    attr.Required,
		})
	}
	if len(fields) == 0 {
		return nil, nil
	}

	sort.Slice(fields, func(i, j int) bool { return fields[i].Name < fields[j].Name })

	return &gogen.ConfigurationSchema{
		GoName:      "ProviderConfig",
		Description: a.localName + " provider configuration.",
		Fields:      fields,
	}, nil
}

func (a *Adapter) FetchDataSources(
	ctx context.Context,
	resources []string,
) ([]gogen.DataSourceSchema, error) {
	data, err := a.Fetcher.FetchSchema(ctx, a.source, a.localName, a.version)
	if err != nil {
		return nil, err
	}

	parsed, err := parseProviderSchema(data)
	if err != nil {
		return nil, fmt.Errorf("parse provider schema: %w", err)
	}

	var providerName string
	for k := range parsed.ProviderSchemas {
		providerName = k
		break
	}
	ps := parsed.ProviderSchemas[providerName]

	prefixes := resourcePrefixes(a.localName, resources)

	var all []gogen.DataSourceSchema
	for name, ds := range ps.DataSourceSchemas {
		if !matchesPrefix(name, prefixes) {
			continue
		}
		schema, err := convertDataSource(name, ds)
		if err != nil {
			return nil, fmt.Errorf("convert data source %s: %w", name, err)
		}
		all = append(all, schema)
	}

	return all, nil
}

// resourcePrefixes maps short service names to TF resource name prefixes.
func resourcePrefixes(provider string, resources []string) []string {
	if len(resources) == 0 {
		return nil
	}
	prefixes := make([]string, len(resources))
	for i, r := range resources {
		prefixes[i] = provider + "_" + r
	}
	return prefixes
}

func matchesPrefix(name string, prefixes []string) bool {
	if len(prefixes) == 0 {
		return true
	}
	for _, p := range prefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// tfProviderSchema is the top-level JSON format from "terraform providers schema -json".
type tfProviderSchema struct {
	FormatVersion   string                  `json:"format_version"`
	ProviderSchemas map[string]tfProvSchema `json:"provider_schemas"`
}

type tfProvSchema struct {
	Provider          *tfResourceSchema           `json:"provider"`
	ResourceSchemas   map[string]tfResourceSchema `json:"resource_schemas"`
	DataSourceSchemas map[string]tfResourceSchema `json:"data_source_schemas"`
}

type tfResourceSchema struct {
	Version int64   `json:"version"`
	Block   tfBlock `json:"block"`
}

type tfBlock struct {
	Attributes map[string]tfAttribute `json:"attributes"`
	BlockTypes map[string]tfBlockType `json:"block_types"`
}

type tfAttribute struct {
	Type        json.RawMessage `json:"type"`
	Required    bool            `json:"required"`
	Optional    bool            `json:"optional"`
	Computed    bool            `json:"computed"`
	ForceNew    bool            `json:"force_new"`
	Sensitive   bool            `json:"sensitive"`
	Description string          `json:"description"`
}

type tfBlockType struct {
	NestingMode string  `json:"nesting_mode"`
	Block       tfBlock `json:"block"`
	MinItems    int64   `json:"min_items"`
	MaxItems    int64   `json:"max_items"`
}

func convertDataSource(name string, ds tfResourceSchema) (gogen.DataSourceSchema, error) {
	goName := tfNameToGo(name)

	var inputFields, outputFields []gogen.Field

	for attrName, attr := range ds.Block.Attributes {
		goField := tfAttrNameToGo(attrName)
		if goField == "" {
			continue
		}
		goType := tfTypeToGo(attr.Type)

		field := gogen.Field{
			Name:        goField,
			GoType:      goType,
			Description: attr.Description,
			Required:    attr.Required,
		}

		if attr.Computed && !attr.Optional && !attr.Required {
			outputFields = append(outputFields, field)
		} else {
			inputFields = append(inputFields, field)
		}
	}

	return gogen.DataSourceSchema{
		GoName:       goName,
		CloudType:    name,
		Description:  "",
		InputFields:  inputFields,
		OutputFields: outputFields,
	}, nil
}

func parseProviderSchema(data []byte) (*tfProviderSchema, error) {
	var s tfProviderSchema
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func convertResource(name string, rs tfResourceSchema) (gogen.ResourceSchema, error) {
	goName := tfNameToGo(name)

	var inputFields, outputFields []gogen.Field
	var createOnlyFields []string

	for attrName, attr := range rs.Block.Attributes {
		goField := tfAttrNameToGo(attrName)
		if goField == "" {
			continue
		}
		goType := tfTypeToGo(attr.Type)

		field := gogen.Field{
			Name:        goField,
			GoType:      goType,
			Description: attr.Description,
			Required:    attr.Required,
		}

		if attr.Computed && !attr.Optional && !attr.Required {
			outputFields = append(outputFields, field)
		} else {
			inputFields = append(inputFields, field)
			if attr.ForceNew {
				createOnlyFields = append(createOnlyFields, goField)
			}
		}
	}

	return gogen.ResourceSchema{
		GoName:           goName,
		CloudType:        name,
		Description:      "",
		InputFields:      inputFields,
		OutputFields:     outputFields,
		CreateOnlyFields: createOnlyFields,
	}, nil
}

// tfTypeToGo converts a TF attribute type to a Go type string.
func tfTypeToGo(raw json.RawMessage) string {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return tfPrimitiveToGo(s)
	}

	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err != nil || len(arr) == 0 {
		return "any"
	}

	var kind string
	_ = json.Unmarshal(arr[0], &kind)

	switch kind {
	case "list", "set":
		if len(arr) > 1 {
			return "[]" + tfTypeToGo(arr[1])
		}
		return "[]any"
	case "map":
		if len(arr) > 1 {
			return "map[string]" + tfTypeToGo(arr[1])
		}
		return "map[string]any"
	case "object":
		return "map[string]any"
	default:
		return "any"
	}
}

func tfPrimitiveToGo(s string) string {
	switch s {
	case "string":
		return "string"
	case "number":
		return "float64"
	case "bool":
		return "bool"
	default:
		return s
	}
}

// tfNameToGo converts a TF resource name to a Go identifier.
func tfNameToGo(name string) string {
	idx := strings.IndexByte(name, '_')
	if idx >= 0 {
		name = name[idx+1:]
	}

	var b strings.Builder
	capNext := true
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c == '_' {
			capNext = true
			continue
		}
		if capNext {
			if i+1 < len(name) && name[i+1] >= 'A' && name[i+1] <= 'Z' {
				b.WriteByte(c)
			} else {
				b.WriteByte(toUpper(c))
			}
			capNext = false
		} else {
			b.WriteByte(c)
		}
	}
	return b.String()
}

func toUpper(c byte) byte {
	if c >= 'a' && c <= 'z' {
		return c - 32
	}
	return c
}

// tfAttrNameToGo converts a TF attribute name to a Go field name.
// Returns an empty string when the input cannot be mapped to a valid Go
// identifier (all underscores, leading digits, etc.).
func tfAttrNameToGo(name string) string {
	parts := strings.Split(name, "_")
	var b strings.Builder
	for _, p := range parts {
		if p == "" {
			continue
		}
		upper := strings.ToUpper(p)
		if acronyms[upper] {
			b.WriteString(upper)
		} else {
			b.WriteString(strings.ToUpper(p[:1]))
			b.WriteString(strings.ToLower(p[1:]))
		}
	}
	result := b.String()
	if result == "" {
		return result
	}
	// Leading digit is not a valid Go identifier.
	if result[0] >= '0' && result[0] <= '9' {
		return ""
	}
	// Check for any non-identifier characters.
	for i := 0; i < len(result); i++ {
		c := result[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '_' {
			continue
		}
		return ""
	}
	return result
}

var acronyms = map[string]bool{
	"ARN": true, "URL": true, "CIDR": true,
	"DNS": true, "HTTP": true, "HTTPS": true, "IAM": true,
	"KMS": true, "SSE": true, "SSO": true,
}
