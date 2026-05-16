package gogen

import (
	"bytes"
	"fmt"
	"go/format"
	"sort"
	"strings"

	"github.com/cloudboss/unobin/pkg/lang"
)

// ResourceFile renders a Go source file for one resource into the resources/
// sub-package.
func ResourceFile(rs ResourceSchema, from string) ([]byte, error) {
	var b bytes.Buffer

	writeGeneratedComment(&b, from)
	b.WriteString("package resources\n\n")
	b.WriteString("import (\n")
	b.WriteString(`	"context"` + "\n")
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
		tag := MapstructureTag(f.Name)
		fmt.Fprintf(&b, "\t%s %s `mapstructure:\"%s\"`\n", f.Name, goType, tag)
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
		tag := MapstructureTag(f.Name)
		fmt.Fprintf(&b, "\t%s %s `mapstructure:\"%s\"`\n", f.Name, f.GoType, tag)
	}
	if len(rs.OutputFields) == 0 {
		b.WriteString("\t// No read-only fields\n")
	}
	b.WriteString("}\n\n")

	fmt.Fprintf(&b, "func (r *%s) ReplaceFields() []string {\n", rs.GoName)
	if len(rs.CreateOnlyFields) > 0 {
		b.WriteString("\treturn []string{\n")
		for _, f := range rs.CreateOnlyFields {
			tag := MapstructureTag(f)
			fmt.Fprintf(&b, "\t\t\"%s\",\n", tag)
		}
		b.WriteString("\t}\n")
	} else {
		b.WriteString("\treturn nil\n")
	}
	b.WriteString("}\n\n")

	for _, op := range []struct {
		method, params, returns string
	}{
		{"Create", "ctx context.Context", "(any, error)"},
		{"Read", "ctx context.Context, priorOutputs any", "(any, error)"},
		{"Update", "ctx context.Context, priorOutputs any", "(any, error)"},
		{"Delete", "ctx context.Context, priorOutputs any", "error"},
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
		tag := MapstructureTag(f.Name)
		fmt.Fprintf(&b, "\t%s %s `mapstructure:\"%s\"`\n", f.Name, goType, tag)
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
		tag := MapstructureTag(f.Name)
		fmt.Fprintf(&b, "\t%s %s `mapstructure:\"%s\"`\n", f.Name, f.GoType, tag)
	}
	b.WriteString("}\n\n")

	fmt.Fprintf(&b, "func (d *%s) Read(ctx context.Context) (any, error) {\n", ds.GoName)
	b.WriteString("\tpanic(\"not implemented\")\n")
	b.WriteString("}\n")

	raw := b.Bytes()
	out, err := format.Source(raw)
	if err != nil {
		return nil, fmt.Errorf("format: %w\n\nraw source:\n%s", err, raw)
	}
	return out, nil
}

// ModuleFile renders a module.go that registers all resources and data
// sources. It lives in the root package and imports the resources/ and
// data/ sub-packages (only the ones that have content).
func ModuleFile(
	packageName string,
	resources []ResourceSchema,
	dataSources []DataSourceSchema,
	modulePath, from string,
) ([]byte, error) {
	var b bytes.Buffer

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
	b.WriteString(")\n\n")

	b.WriteString("func Module() *runtime.Module {\n")
	b.WriteString("\treturn &runtime.Module{\n")
	fmt.Fprintf(&b, "\t\tName:        \"%s\",\n", packageName)
	fmt.Fprintf(&b, "\t\tDescription: \"Generated %s module\",\n", packageName)

	if len(resources) > 0 {
		sort.Slice(resources, func(i, j int) bool {
			identI := lang.PascalToKebab(resources[i].GoName)
			identJ := lang.PascalToKebab(resources[j].GoName)
			return identI < identJ
		})
		b.WriteString("\t\tResources: map[string]runtime.ResourceType{\n")
		for _, rs := range resources {
			typeKey := lang.PascalToKebab(rs.GoName)
			fmt.Fprintf(&b, "\t\t\t\"%s\": {\n", typeKey)
			fmt.Fprintf(&b, "\t\t\t\tName:          \"%s\",\n", typeKey)
			desc := escapeQuote(rs.Description)
			fmt.Fprintf(&b, "\t\t\t\tDescription:   \"%s\",\n", desc)
			b.WriteString("\t\t\t\tSchemaVersion: 1,\n")
			fmt.Fprintf(&b, "\t\t\t\tNew:           func() runtime.Resource { return &resources.%s{} },\n",
				rs.GoName)
			b.WriteString("\t\t\t},\n")
		}
		b.WriteString("\t\t},\n")
	}

	if len(dataSources) > 0 {
		sort.Slice(dataSources, func(i, j int) bool {
			identI := lang.PascalToKebab(dataSources[i].GoName)
			identJ := lang.PascalToKebab(dataSources[j].GoName)
			return identI < identJ
		})
		b.WriteString("\t\tDataSources: map[string]runtime.DataSourceType{\n")
		for _, ds := range dataSources {
			typeKey := lang.PascalToKebab(ds.GoName)
			fmt.Fprintf(&b, "\t\t\t\"%s\": {\n", typeKey)
			fmt.Fprintf(&b, "\t\t\t\tName:        \"%s\",\n", typeKey)
			desc := escapeQuote(ds.Description)
			fmt.Fprintf(&b, "\t\t\t\tDescription: \"%s\",\n", desc)
			fmt.Fprintf(&b, "\t\t\t\tNew:         func() runtime.DataSource { return &data.%s{} },\n",
				ds.GoName)
			b.WriteString("\t\t\t},\n")
		}
		b.WriteString("\t\t},\n")
	}

	b.WriteString("\t}\n")
	b.WriteString("}\n")

	return format.Source(b.Bytes())
}

// GoMod renders a go.mod file for a generated module. When replaceUnobin is
// non-empty, its absolute path is used to add a replace directive for the
// unobin dependency.
func GoMod(modulePath, replaceUnobin string) ([]byte, error) {
	var b bytes.Buffer
	fmt.Fprintf(&b, "module %s\n\n", modulePath)
	b.WriteString("go 1.26\n\n")
	b.WriteString("require (\n")
	b.WriteString("\tgithub.com/cloudboss/unobin v0.0.0\n")
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
	fmt.Fprintf(b, "// Generated by unobin generate gomodule --from %s.\n\n", from)
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
