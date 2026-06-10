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
		for _, fl := range cfg.Describe(lib.Configuration) {
			fmt.Fprintf(out, "      %s: %s", fl.Name, fieldTypeLabel(fl))
			if fl.Description != "" {
				fmt.Fprintf(out, "  -- %s", fl.Description)
			}
			fmt.Fprintln(out)
		}
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
	if outPath == "" {
		_, err := cmd.OutOrStdout().Write(buf.Bytes())
		return err
	}
	return ufs.WriteFileAtomic(outPath, buf.Bytes(), 0o644)
}

func renderSchemaTemplate(out io.Writer, f *lang.File, dag *runtime.DAG, info Info) {
	fmt.Fprintln(out, "factory: {")
	if info.LibraryPath != "" {
		fmt.Fprintf(out, "  library-path: '%s'\n", info.LibraryPath)
	}
	fmt.Fprintf(out,
		"  supported-versions: [\n    { version: '%s', content-revision: '%s' },\n  ]\n}\n",
		info.FactoryVersion, info.ContentRevision)
	fmt.Fprintln(out)
	fmt.Fprintln(out, "state: {")
	fmt.Fprintln(out, "  @backend: local")
	fmt.Fprintln(out, "  path: '.unobin/state'")
	fmt.Fprintln(out, "  encryption: {")
	fmt.Fprintln(out, "    @key-source: noop")
	fmt.Fprintln(out, "  }")
	fmt.Fprintln(out, "}")
	inputs := topLevelObject(f, "inputs")
	if inputs != nil && len(inputs.Fields) > 0 {
		fmt.Fprintln(out)
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
				fmt.Fprintf(out, "  # %s\n", description)
			}
			fmt.Fprintf(out, "  %s: %s  # type: %s\n",
				fld.Key.Name, placeholderForType(typeExpr), printType(typeExpr))
		}
		fmt.Fprintln(out, "}")
	}
	renderConfigurationsTemplate(out, f, dag, info)
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
	fmt.Fprintln(out)
	fmt.Fprintln(out, "configurations: {")
	for _, alias := range aliases {
		fmt.Fprintf(out, "  %s: {\n", alias)
		fields := cfg.Describe(info.Libraries[alias].Configuration)
		for _, name := range owedByAlias[alias] {
			fmt.Fprintf(out, "    %s: {\n", name)
			for _, fl := range fields {
				if fl.Description != "" {
					fmt.Fprintf(out, "      # %s\n", fl.Description)
				}
				fmt.Fprintf(out, "      %s: %s  # type: %s\n",
					fl.Name, placeholderForFieldType(fl.Type), fieldTypeLabel(fl))
			}
			fmt.Fprintln(out, "    }")
		}
		fmt.Fprintln(out, "  }")
	}
	fmt.Fprintln(out, "}")
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
