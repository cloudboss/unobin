package runner

import (
	"bytes"
	"fmt"
	"io"
	"slices"
	"strings"

	ufs "github.com/cloudboss/unobin/pkg/fs"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
	"github.com/spf13/cobra"
)

func newSchemaCmd(info Info) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "schema",
		Short: "Print the factory's input declarations",
		RunE: func(cmd *cobra.Command, args []string) error {
			return doSchema(cmd, info)
		},
	}
	var outPath string
	tmpl := &cobra.Command{
		Use:   "template",
		Short: "Print a starter config.ub for this factory",
		RunE: func(cmd *cobra.Command, args []string) error {
			return doSchemaTemplate(cmd, info, outPath)
		},
	}
	tmpl.Flags().StringVarP(&outPath, "out", "o", "",
		"Write the template to this file instead of stdout.")
	cmd.AddCommand(tmpl)
	return cmd
}

func doSchema(cmd *cobra.Command, info Info) error {
	f, dag, err := parsedFile(info)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	inputs := topLevelObject(f, "inputs")
	if inputs == nil || len(inputs.Fields) == 0 {
		fmt.Fprintln(out, "No inputs declared.")
		printOutputSchema(out, f)
		printConfigurationSchema(out, f, dag, info)
		return nil
	}
	for _, fld := range inputs.Fields {
		if fld.Key.Kind != lang.FieldIdent {
			continue
		}
		decl, ok := fld.Value.(*lang.ObjectLit)
		if !ok {
			continue
		}
		typeStr := "?"
		var description string
		for _, df := range decl.Fields {
			if df.Key.Kind != lang.FieldIdent {
				continue
			}
			switch df.Key.Name {
			case "type":
				typeStr = printType(df.Value)
			case "description":
				if s, ok := df.Value.(*lang.StringLit); ok {
					description = s.Value
				}
			}
		}
		fmt.Fprintf(out, "%s: %s", fld.Key.Name, typeStr)
		if description != "" {
			fmt.Fprintf(out, "  -- %s", description)
		}
		fmt.Fprintln(out)
	}
	printOutputSchema(out, f)
	printConfigurationSchema(out, f, dag, info)
	return nil
}

// printOutputSchema lists the factory's declared outputs: each name
// with its sensitivity marker and declared description. Values are
// runtime results, so only the metadata prints here.
func printOutputSchema(out io.Writer, f *lang.File) {
	outputs := topLevelObject(f, "outputs")
	if outputs == nil || len(outputs.Fields) == 0 {
		return
	}
	sensitive := lang.SensitiveOutputs(outputs)
	fmt.Fprintln(out)
	fmt.Fprintln(out, "outputs:")
	for _, fld := range outputs.Fields {
		if fld.Key.Kind != lang.FieldIdent || fld.Key.IsMeta() {
			continue
		}
		fmt.Fprintf(out, "  %s", fld.Key.Name)
		if sensitive[fld.Key.Name] {
			fmt.Fprint(out, " (sensitive)")
		}
		if d := lang.OutputDescription(fld.Value); d != "" {
			fmt.Fprintf(out, "  -- %s", d)
		}
		fmt.Fprintln(out)
	}
}

// printConfigurationSchema lists each configured library: the names
// the factory defines internally, the names config.ub must supply
// (every selection some node makes that is not internal), and the
// configuration's fields.
func printConfigurationSchema(out io.Writer, f *lang.File, dag *runtime.DAG, info Info) {
	var aliases []string
	for alias, lib := range info.Libraries {
		if lib.Configuration != nil {
			aliases = append(aliases, alias)
		}
	}
	if len(aliases) == 0 {
		return
	}
	slices.Sort(aliases)
	used := dag.ConfigurationSelections(info.Libraries)
	internal := runtime.InternalConfigurationNames(f)
	fmt.Fprintln(out)
	fmt.Fprintln(out, "configurations:")
	for _, alias := range aliases {
		lib := info.Libraries[alias]
		fmt.Fprintf(out, "  %s:", alias)
		if d := lib.Configuration.Description; d != "" {
			fmt.Fprintf(out, "  -- %s", d)
		}
		fmt.Fprintln(out)
		if names := sortedSetKeys(internal[alias]); len(names) > 0 {
			fmt.Fprintf(out, "    internal: %s\n", strings.Join(names, ", "))
		}
		if owed := owedNames(used[alias], internal[alias]); len(owed) > 0 {
			fmt.Fprintf(out, "    needed from config.ub: %s\n", strings.Join(owed, ", "))
		}
		writeShowFields(out, cfg.Describe(lib.Configuration), "      ")
	}
}

// writeShowFields prints configuration fields one per line, indenting
// an object field's own fields beneath it.
func writeShowFields(out io.Writer, fields []cfg.Field, indent string) {
	for _, fl := range fields {
		fmt.Fprintf(out, "%s%s: %s", indent, fl.Name, fieldTypeLabel(fl))
		if fl.Description != "" {
			fmt.Fprintf(out, "  -- %s", fl.Description)
		}
		fmt.Fprintln(out)
		writeShowFields(out, fl.Fields, indent+"  ")
	}
}

// owedNames returns the selections in used that the factory does not
// define internally, sorted: the names config.ub must supply.
func owedNames(used, internal map[string]bool) []string {
	var owed []string
	for name := range used {
		if !internal[name] {
			owed = append(owed, name)
		}
	}
	slices.Sort(owed)
	return owed
}

func sortedSetKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func fieldTypeLabel(f cfg.Field) string {
	if f.Optional {
		return "optional(" + f.Type + ")"
	}
	return f.Type
}

func doSchemaTemplate(cmd *cobra.Command, info Info, outPath string) error {
	f, dag, err := parsedFile(info)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	renderSchemaTemplate(&buf, f, dag, info)
	formatted, err := lang.Canonicalize("config.ub", buf.Bytes())
	if err != nil {
		return err
	}
	if outPath == "" {
		_, err := cmd.OutOrStdout().Write(formatted)
		return err
	}
	return ufs.WriteFileAtomic(outPath, formatted, 0o644)
}

// renderSchemaTemplate emits a draft config for the formatter:
// Canonicalize owns indentation and alignment, so the draft spells only
// the structure, with line breaks marking the blocks that stay
// expanded.
func renderSchemaTemplate(out io.Writer, f *lang.File, dag *runtime.DAG, info Info) {
	fmt.Fprintln(out, "factory: {")
	fmt.Fprint(out, renderPinBlock(info.LibraryPath, info.FactoryVersion, info.ContentRevision))
	renderConfigurationsTemplate(out, f, dag, info)
	renderInputsTemplate(out, f)
	fmt.Fprintln(out, "}")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "state: {")
	fmt.Fprintln(out, "@backend: local")
	fmt.Fprintln(out, "path: '.unobin/state'")
	fmt.Fprintln(out, "}")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "encryption: {")
	fmt.Fprintln(out, "@key-source: noop")
	fmt.Fprintln(out, "}")
}

// renderInputsTemplate scaffolds the factory.inputs block: one
// placeholder line per declared input, with its description and type
// alongside.
func renderInputsTemplate(out io.Writer, f *lang.File) {
	inputs := topLevelObject(f, "inputs")
	if inputs == nil || len(inputs.Fields) == 0 {
		return
	}
	fmt.Fprintln(out, "inputs: {")
	for _, fld := range inputs.Fields {
		if fld.Key.Kind != lang.FieldIdent {
			continue
		}
		decl, ok := fld.Value.(*lang.ObjectLit)
		if !ok {
			continue
		}
		var typeExpr lang.Expr
		var description string
		for _, df := range decl.Fields {
			if df.Key.Kind != lang.FieldIdent {
				continue
			}
			switch df.Key.Name {
			case "type":
				typeExpr = df.Value
			case "description":
				if s, ok := df.Value.(*lang.StringLit); ok {
					description = s.Value
				}
			}
		}
		if description != "" {
			fmt.Fprintf(out, "# %s\n", description)
		}
		fmt.Fprintf(out, "%s: %s  # type: %s\n",
			fld.Key.Name, placeholderForType(typeExpr), printType(typeExpr))
	}
	fmt.Fprintln(out, "}")
}

// renderConfigurationsTemplate scaffolds the configurations the
// operator owes: every selection some node makes that the factory
// does not define internally, with a placeholder per field.
func renderConfigurationsTemplate(out io.Writer, f *lang.File, dag *runtime.DAG, info Info) {
	used := dag.ConfigurationSelections(info.Libraries)
	internal := runtime.InternalConfigurationNames(f)
	owedByAlias := map[string][]string{}
	var aliases []string
	for alias, names := range used {
		if owed := owedNames(names, internal[alias]); len(owed) > 0 {
			owedByAlias[alias] = owed
			aliases = append(aliases, alias)
		}
	}
	if len(aliases) == 0 {
		return
	}
	slices.Sort(aliases)
	fmt.Fprintln(out, "configurations: {")
	for _, alias := range aliases {
		fields := cfg.Describe(info.Libraries[alias].Configuration)
		for _, name := range owedByAlias[alias] {
			fmt.Fprintf(out, "%s.%s: {\n", alias, name)
			writeTemplateFields(out, fields)
			fmt.Fprintln(out, "}")
		}
	}
	fmt.Fprintln(out, "}")
}

// writeTemplateFields scaffolds one placeholder line per
// configuration field. An object field opens a block and scaffolds
// its own fields inside; its type comment goes on its own line above
// the field, so optionality stays visible where the canonical form
// keeps it.
func writeTemplateFields(out io.Writer, fields []cfg.Field) {
	for _, fl := range fields {
		if fl.Description != "" {
			fmt.Fprintf(out, "# %s\n", fl.Description)
		}
		if len(fl.Fields) > 0 {
			fmt.Fprintf(out, "# type: %s\n", fieldTypeLabel(fl))
			fmt.Fprintf(out, "%s: {\n", fl.Name)
			writeTemplateFields(out, fl.Fields)
			fmt.Fprintln(out, "}")
			continue
		}
		fmt.Fprintf(out, "%s: %s  # type: %s\n",
			fl.Name, placeholderForFieldType(fl.Type), fieldTypeLabel(fl))
	}
}

// placeholderForFieldType picks a starter value for one configuration
// field by its language type label.
func placeholderForFieldType(t string) string {
	switch {
	case t == "string":
		return "''"
	case t == "integer" || t == "number":
		return "0"
	case t == "boolean":
		return "false"
	case strings.HasPrefix(t, "list("):
		return "[]"
	case strings.HasPrefix(t, "map(") || t == "object":
		return "{}"
	}
	return "null"
}

func placeholderForType(e lang.Expr) string {
	switch v := e.(type) {
	case *lang.Ident:
		switch v.Name {
		case "string":
			return "''"
		case "integer", "number":
			return "0"
		case "boolean":
			return "false"
		}
	case *lang.Call:
		if v.Callee != nil {
			switch v.Callee.Name {
			case "list":
				return "[]"
			case "map":
				return "{}"
			}
		}
	}
	return "null"
}

func topLevelArray(f *lang.File, name string) *lang.ArrayLit {
	if f == nil || f.Body == nil {
		return nil
	}
	for _, fld := range f.Body.Fields {
		if fld.Key.Kind == lang.FieldIdent && fld.Key.Name == name {
			if arr, ok := fld.Value.(*lang.ArrayLit); ok {
				return arr
			}
			return nil
		}
	}
	return nil
}

func topLevelObject(f *lang.File, name string) *lang.ObjectLit {
	if f == nil || f.Body == nil {
		return nil
	}
	for _, fld := range f.Body.Fields {
		if fld.Key.Kind == lang.FieldIdent && fld.Key.Name == name {
			if obj, ok := fld.Value.(*lang.ObjectLit); ok {
				return obj
			}
			return nil
		}
	}
	return nil
}

// printType renders a parsed type expression back to its source form
// (e.g., `optional(list(string))`). It stays separate from lang.Render
// because Render formats evaluated Go values rather than AST nodes.
func printType(e lang.Expr) string {
	switch v := e.(type) {
	case *lang.Ident:
		return v.Name
	case *lang.Call:
		args := make([]string, len(v.Args))
		for i, a := range v.Args {
			args[i] = printType(a)
		}
		callee := ""
		if v.Callee != nil {
			callee = v.Callee.Name
		}
		return callee + "(" + strings.Join(args, ", ") + ")"
	case *lang.NumberLit:
		return v.Value
	case *lang.StringLit:
		return "'" + v.Value + "'"
	case *lang.BoolLit:
		if v.Value {
			return "true"
		}
		return "false"
	case *lang.NullLit:
		return "null"
	case *lang.ArrayLit:
		args := make([]string, len(v.Elements))
		for i, el := range v.Elements {
			args[i] = printType(el)
		}
		return "[" + strings.Join(args, ", ") + "]"
	}
	return "?"
}
