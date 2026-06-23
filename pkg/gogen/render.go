package gogen

import (
	"bytes"
	"cmp"
	"fmt"
	"go/format"
	"slices"
	"strings"

	"golang.org/x/mod/semver"

	"github.com/cloudboss/unobin/pkg/lang"
)

// ResourceFile renders a Go source file for one resource into the resources/
// sub-package.
func ResourceFile(rs ResourceSchema, from string) ([]byte, error) {
	var b bytes.Buffer

	writeGeneratedComment(&b, from)
	b.WriteString("package resources\n\n")
	b.WriteString("import (\n")
	b.WriteString(`	"context"` + "\n\n")
	b.WriteString(`	"github.com/cloudboss/unobin/pkg/runtime"` + "\n")
	b.WriteString(")\n\n")

	if rs.Description != "" {
		for _, line := range wordWrap(sanitizeComment(rs.Description), 80) {
			fmt.Fprintf(&b, "// %s\n", line)
		}
	}
	fmt.Fprintf(&b, "// %s implements runtime.Resource for %s.\n", rs.GoName, rs.CloudType)
	fmt.Fprintf(&b, "type %s struct {\n", rs.GoName)
	for _, f := range rs.InputFields {
		if f.Name == "" {
			continue
		}
		if f.Description != "" {
			fmt.Fprintf(&b, "\t// %s\n", sanitizeComment(f.Description))
		}
		goType := f.GoType
		if !f.Required {
			goType = PointerType(goType)
		}
		tag := UBTag(f.Name)
		fmt.Fprintf(&b, "\t%s %s `ub:\"%s\"`\n", f.Name, goType, tag)
	}
	if len(rs.InputFields) == 0 {
		b.WriteString("\t// No writable fields\n")
	}
	b.WriteString("}\n\n")

	outName := rs.GoName + "Output"
	fmt.Fprintf(&b, "// %s holds the read-only state for %s.\n", outName, rs.CloudType)
	fmt.Fprintf(&b, "type %s struct {\n", outName)
	for _, f := range rs.OutputFields {
		if f.Name == "" {
			continue
		}
		if f.Description != "" {
			fmt.Fprintf(&b, "\t// %s\n", sanitizeComment(f.Description))
		}
		tag := UBTag(f.Name)
		fmt.Fprintf(&b, "\t%s %s `ub:\"%s\"`\n", f.Name, f.GoType, tag)
	}
	if len(rs.OutputFields) == 0 {
		b.WriteString("\t// No read-only fields\n")
	}
	b.WriteString("}\n\n")

	fmt.Fprintf(&b, "func (r *%s) SchemaVersion() int { return 1 }\n\n", rs.GoName)

	fmt.Fprintf(&b, "func (r *%s) ReplaceFields() []string {\n", rs.GoName)
	if len(rs.CreateOnlyFields) > 0 {
		b.WriteString("\treturn []string{\n")
		for _, f := range rs.CreateOnlyFields {
			tag := UBTag(f)
			fmt.Fprintf(&b, "\t\t\"%s\",\n", tag)
		}
		b.WriteString("\t}\n")
	} else {
		b.WriteString("\treturn nil\n")
	}
	b.WriteString("}\n\n")

	outPtr := "*" + outName
	for _, op := range []struct {
		method, params, returns string
	}{
		{"Create", "ctx context.Context, cfg any", "(" + outPtr + ", error)"},
		{"Read", "ctx context.Context, cfg any, priorOutputs " + outPtr, "(" + outPtr + ", error)"},
		{
			"Update",
			"ctx context.Context, cfg any, prior runtime.Prior[" + rs.GoName + ", " + outPtr + "]",
			"(" + outPtr + ", error)",
		},
		{"Delete", "ctx context.Context, cfg any, priorOutputs " + outPtr, "error"},
	} {
		b.WriteString(writeStub(rs.GoName, op.method, op.params, op.returns))
	}

	raw := b.Bytes()
	out, err := format.Source(raw)
	if err != nil {
		return nil, fmt.Errorf("format: %w\n\nraw source:\n%s", err, raw)
	}
	return out, nil
}

// DataSourceFile renders a Go source file for one data source into the data/
// sub-package.
func DataSourceFile(ds DataSourceSchema, from string) ([]byte, error) {
	var b bytes.Buffer

	writeGeneratedComment(&b, from)
	b.WriteString("package data\n\n")
	b.WriteString("import (\n")
	b.WriteString(`	"context"` + "\n")
	b.WriteString(")\n\n")

	if ds.Description != "" {
		for _, line := range wordWrap(sanitizeComment(ds.Description), 80) {
			fmt.Fprintf(&b, "// %s\n", line)
		}
	}
	fmt.Fprintf(&b, "// %s implements runtime.DataSource for %s.\n", ds.GoName, ds.CloudType)
	fmt.Fprintf(&b, "type %s struct {\n", ds.GoName)
	for _, f := range ds.InputFields {
		if f.Name == "" {
			continue
		}
		if f.Description != "" {
			fmt.Fprintf(&b, "\t// %s\n", sanitizeComment(f.Description))
		}
		goType := f.GoType
		if !f.Required {
			goType = PointerType(goType)
		}
		tag := UBTag(f.Name)
		fmt.Fprintf(&b, "\t%s %s `ub:\"%s\"`\n", f.Name, goType, tag)
	}
	b.WriteString("}\n\n")

	outName := ds.GoName + "Output"
	fmt.Fprintf(&b, "// %s holds the read-only state for %s.\n", outName, ds.CloudType)
	fmt.Fprintf(&b, "type %s struct {\n", outName)
	for _, f := range ds.OutputFields {
		if f.Name == "" {
			continue
		}
		if f.Description != "" {
			fmt.Fprintf(&b, "\t// %s\n", sanitizeComment(f.Description))
		}
		tag := UBTag(f.Name)
		fmt.Fprintf(&b, "\t%s %s `ub:\"%s\"`\n", f.Name, f.GoType, tag)
	}
	b.WriteString("}\n\n")

	fmt.Fprintf(&b, "func (d *%s) Read(ctx context.Context, cfg any) (*%s, error) {\n",
		ds.GoName, outName)
	b.WriteString("\tpanic(\"not implemented\")\n")
	b.WriteString("}\n")

	raw := b.Bytes()
	out, err := format.Source(raw)
	if err != nil {
		return nil, fmt.Errorf("format: %w\n\nraw source:\n%s", err, raw)
	}
	return out, nil
}

// LibraryFile renders a library.go that registers all resources and data
// sources. It lives in the root package and imports the resources/ and
// data/ sub-packages (only the ones that have content). configuration
// may be nil; when present and non-empty, the registration references
// the ProviderConfig struct declared in configuration.go.
func LibraryFile(
	packageName string,
	resources []ResourceSchema,
	dataSources []DataSourceSchema,
	configuration *ConfigurationSchema,
	modulePath, from string,
) ([]byte, error) {
	var b bytes.Buffer

	hasConfig := configuration != nil && len(configuration.Fields) > 0

	writeGeneratedComment(&b, from)
	fmt.Fprintf(&b, "package %s\n\n", packageName)

	b.WriteString("import (\n")
	if len(resources) > 0 {
		fmt.Fprintf(&b, "\t%q\n", modulePath+"/resources")
	}
	if len(dataSources) > 0 {
		fmt.Fprintf(&b, "\t%q\n", modulePath+"/data")
	}
	b.WriteString("\n")
	b.WriteString(`	"github.com/cloudboss/unobin/pkg/runtime"` + "\n")
	if hasConfig {
		b.WriteString(`	"github.com/cloudboss/unobin/pkg/sdk/cfg"` + "\n")
	}
	b.WriteString(")\n\n")

	b.WriteString("func Library() *runtime.Library {\n")
	b.WriteString("\treturn &runtime.Library{\n")
	fmt.Fprintf(&b, "\t\tName:        \"%s\",\n", packageName)
	fmt.Fprintf(&b, "\t\tLibraryPath: \"%s\",\n", modulePath)
	fmt.Fprintf(&b, "\t\tDescription: \"Generated %s library\",\n", packageName)

	if hasConfig {
		b.WriteString("\t\tConfiguration: &cfg.ConfigurationType[any]{\n")
		desc := escapeQuote(configuration.Description)
		if desc == "" {
			desc = packageName + " provider configuration."
		}
		fmt.Fprintf(&b, "\t\t\tDescription: \"%s\",\n", desc)
		fmt.Fprintf(&b, "\t\t\tNew:         func() any { return &%s{} },\n",
			configuration.GoName)
		b.WriteString("\t\t},\n")
	}

	if len(resources) > 0 {
		slices.SortFunc(resources, func(a, b ResourceSchema) int {
			return cmp.Compare(lang.PascalToKebab(a.GoName), lang.PascalToKebab(b.GoName))
		})
		b.WriteString("\t\tResources: map[string]runtime.ResourceRegistration{\n")
		for _, rs := range resources {
			typeKey := lang.PascalToKebab(rs.GoName)
			fmt.Fprintf(&b,
				"\t\t\t\"%s\": runtime.MakeResource[resources.%s, *resources.%sOutput, any](),\n",
				typeKey, rs.GoName, rs.GoName)
		}
		b.WriteString("\t\t},\n")
	}

	if len(dataSources) > 0 {
		slices.SortFunc(dataSources, func(a, b DataSourceSchema) int {
			return cmp.Compare(lang.PascalToKebab(a.GoName), lang.PascalToKebab(b.GoName))
		})
		b.WriteString("\t\tDataSources: map[string]runtime.DataSourceRegistration{\n")
		for _, ds := range dataSources {
			typeKey := lang.PascalToKebab(ds.GoName)
			fmt.Fprintf(&b,
				"\t\t\t\"%s\": runtime.MakeDataSource[data.%s, *data.%sOutput, any](),\n",
				typeKey, ds.GoName, ds.GoName)
		}
		b.WriteString("\t\t},\n")
	}

	b.WriteString("\t}\n")
	b.WriteString("}\n")

	return format.Source(b.Bytes())
}

// ConfigurationFile renders a configuration.go that declares the
// library-level provider config struct. Each field is wrapped in the
// cfg.* type matching its primitive Go type; non-required fields use
// a pointer so the decoder treats them as optional.
func ConfigurationFile(cs ConfigurationSchema, packageName, from string) ([]byte, error) {
	var b bytes.Buffer

	writeGeneratedComment(&b, from)
	fmt.Fprintf(&b, "package %s\n\n", packageName)
	b.WriteString("import (\n")
	b.WriteString(`	"github.com/cloudboss/unobin/pkg/sdk/cfg"` + "\n")
	b.WriteString(")\n\n")

	if cs.Description != "" {
		for _, line := range wordWrap(sanitizeComment(cs.Description), 80) {
			fmt.Fprintf(&b, "// %s\n", line)
		}
	}
	fmt.Fprintf(&b, "// %s is the operator-facing body for configurations such as\n", cs.GoName)
	fmt.Fprintf(&b, "// %s { ... } or name: %s { ... }.\n", packageName, packageName)
	fmt.Fprintf(&b, "type %s struct {\n", cs.GoName)
	for _, f := range cs.Fields {
		if f.Name == "" {
			continue
		}
		if f.Description != "" {
			fmt.Fprintf(&b, "\t// %s\n", sanitizeComment(f.Description))
		}
		wrapper := cfgWrapperType(f.GoType)
		if !f.Required {
			wrapper = "*" + wrapper
		}
		fmt.Fprintf(&b, "\t%s %s\n", f.Name, wrapper)
	}
	if len(cs.Fields) == 0 {
		b.WriteString("\t// No configuration fields\n")
	}
	b.WriteString("}\n")

	raw := b.Bytes()
	out, err := format.Source(raw)
	if err != nil {
		return nil, fmt.Errorf("format: %w\n\nraw source:\n%s", err, raw)
	}
	return out, nil
}

// cfgWrapperType maps a primitive Go type produced by a SchemaAdapter to
// the corresponding cfg.* wrapper. Unknown types fall back to cfg.Any so
// the generated file still compiles; the library author can refine the
// type by hand.
func cfgWrapperType(goType string) string {
	switch goType {
	case "string":
		return "cfg.String"
	case "int64":
		return "cfg.Integer"
	case "float64":
		return "cfg.Number"
	case "bool":
		return "cfg.Boolean"
	}
	if strings.HasPrefix(goType, "[]") {
		return "cfg.List[" + cfgWrapperType(goType[2:]) + "]"
	}
	if strings.HasPrefix(goType, "map[string]") {
		return "cfg.Map[" + cfgWrapperType(goType[len("map[string]"):]) + "]"
	}
	return "cfg.Any"
}

// GoMod renders a go.mod file for a generated library. unobinVersion
// pins the unobin requirement when it is a release version; otherwise
// the v0.0.0 placeholder stands in, which only resolves with a
// replace. When replaceUnobin is non-empty, its absolute path is used
// to add a replace directive for the unobin dependency.
func GoMod(modulePath, replaceUnobin, unobinVersion string) ([]byte, error) {
	if !semver.IsValid(unobinVersion) {
		unobinVersion = "v0.0.0"
	}
	var b bytes.Buffer
	fmt.Fprintf(&b, "module %s\n\n", modulePath)
	b.WriteString("go 1.26\n\n")
	b.WriteString("require (\n")
	fmt.Fprintf(&b, "\tgithub.com/cloudboss/unobin %s\n", unobinVersion)
	b.WriteString(")\n")
	if replaceUnobin != "" {
		b.WriteString("\n")
		fmt.Fprintf(&b, "replace github.com/cloudboss/unobin => %s\n", replaceUnobin)
	}
	return b.Bytes(), nil
}

func writeGeneratedComment(b *bytes.Buffer, from string) {
	if from == "" {
		from = "unknown"
	}
	fmt.Fprintf(b, "// Generated by unobin generate golibrary --from %s.\n\n", from)
}

func writeStub(goName, method, params, returns string) string {
	var b bytes.Buffer

	fmt.Fprintf(&b, "func (r *%s) %s(%s) %s {\n", goName, method, params, returns)
	b.WriteString("\tpanic(\"not implemented\")\n")
	b.WriteString("}\n\n")
	return b.String()
}

func escapeQuote(s string) string {
	return strings.ReplaceAll(s, `"`, `\"`)
}

// sanitizeComment replaces newlines and other control characters that would
// break a //-style Go comment.
func sanitizeComment(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	return strings.Join(strings.Fields(s), " ")
}

func wordWrap(s string, maxLen int) []string {
	if len(s) <= maxLen {
		return []string{s}
	}
	var lines []string
	words := strings.Fields(s)
	cur := ""
	for _, w := range words {
		if cur == "" {
			cur = w
			continue
		}
		if len(cur)+1+len(w) > maxLen {
			lines = append(lines, cur)
			cur = w
		} else {
			cur += " " + w
		}
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}
