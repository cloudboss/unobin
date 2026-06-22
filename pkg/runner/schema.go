package runner

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/cloudboss/unobin/pkg/backends"
	"github.com/cloudboss/unobin/pkg/encrypters"
	ufs "github.com/cloudboss/unobin/pkg/fs"
	"github.com/cloudboss/unobin/pkg/lang"
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
		Short: "Print a starter stack file for this factory",
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
	parsed, err := parseFactory(info)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	inputs := parsed.inputs()
	if len(inputs) == 0 {
		fmt.Fprintln(out, "No inputs declared.")
		printOutputSchema(out, parsed)
		return nil
	}
	for _, input := range inputs {
		typeStr := printType(input.typeExpr)
		defaultStr := ""
		if input.defaultExpr != nil {
			defaultStr = printType(input.defaultExpr)
		}
		fmt.Fprintf(out, "%s: %s", input.name, typeStr)
		if defaultStr != "" {
			fmt.Fprintf(out, "  default: %s", defaultStr)
		}
		if input.description != "" {
			fmt.Fprintf(out, "  -- %s", input.description)
		}
		fmt.Fprintln(out)
	}
	printOutputSchema(out, parsed)
	return nil
}

// printOutputSchema lists the factory's declared outputs: each name
// with its sensitivity marker and declared description. Values are
// runtime results, so only the metadata prints here.
func printOutputSchema(out io.Writer, parsed *parsedFactory) {
	outputs := parsed.outputs()
	if len(outputs) == 0 {
		return
	}
	sensitive := parsed.sensitiveOutputs()
	fmt.Fprintln(out)
	fmt.Fprintln(out, "outputs:")
	for _, output := range outputs {
		fmt.Fprintf(out, "  %s", output.name)
		if sensitive[output.name] {
			fmt.Fprint(out, " (sensitive)")
		}
		if d := lang.OutputDescription(output.body); d != "" {
			fmt.Fprintf(out, "  -- %s", d)
		}
		fmt.Fprintln(out)
	}
}

func doSchemaTemplate(cmd *cobra.Command, info Info, outPath string) error {
	parsed, err := parseFactory(info)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	renderSchemaTemplate(&buf, parsed, info)
	formatted, err := lang.Canonicalize("stack.ub", buf.Bytes())
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
func renderSchemaTemplate(out io.Writer, parsed *parsedFactory, info Info) {
	fmt.Fprintln(out, "stack: {")
	fmt.Fprintln(out, "factory: {")
	fmt.Fprint(out, renderPinBlock(info.LibraryPath, info.FactoryVersion, info.ContentRevision))
	renderInputsTemplate(out, parsed)
	fmt.Fprintln(out, "}")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "state: "+backends.LocalName+" {")
	fmt.Fprintln(out, "path: '.unobin/state'")
	fmt.Fprintln(out, "}")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "encryption: "+encrypters.NoopName+" {}")
	fmt.Fprintln(out, "}")
}

// renderInputsTemplate scaffolds the factory.inputs block: one
// placeholder line per declared input, with its description and type
// alongside.
func renderInputsTemplate(out io.Writer, parsed *parsedFactory) {
	inputs := parsed.inputs()
	if len(inputs) == 0 {
		return
	}
	fmt.Fprintln(out, "inputs: {")
	for _, input := range inputs {
		if input.description != "" {
			fmt.Fprintf(out, "# %s\n", input.description)
		}
		fmt.Fprintf(out, "%s: %s  # type: %s\n",
			input.name, placeholderForType(input.typeExpr), printType(input.typeExpr))
	}
	fmt.Fprintln(out, "}")
}

func placeholderForType(e lang.Expr) string {
	switch v := e.(type) {
	case *lang.TypeAtomic:
		switch v.Name {
		case "string":
			return "''"
		case "integer", "number":
			return "0"
		case "boolean":
			return "false"
		}
	case *lang.TypeList:
		return "[]"
	case *lang.TypeMap, *lang.TypeObject:
		return "{}"
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

// printType renders a parsed type expression back to its source form
// (e.g., `optional(list(string))`). It stays separate from lang.Render
// because Render formats evaluated Go values rather than AST nodes.
func printType(e lang.Expr) string {
	switch v := e.(type) {
	case *lang.TypeAtomic:
		return v.Name
	case *lang.TypeList:
		return "list(" + printType(v.Elem) + ")"
	case *lang.TypeMap:
		return "map(" + printType(v.Elem) + ")"
	case *lang.TypeTuple:
		args := make([]string, len(v.Elements))
		for i, elem := range v.Elements {
			args[i] = printType(elem)
		}
		return "tuple(" + strings.Join(args, ", ") + ")"
	case *lang.TypeObject:
		fields := make([]string, len(v.Fields))
		for i, field := range v.Fields {
			fields[i] = field.Name + ": " + printTypeObjectField(field)
		}
		out := "object({ " + strings.Join(fields, ", ") + " })"
		if v.Open {
			return "open(" + out + ")"
		}
		return out
	case *lang.TypeOptional:
		return "optional(" + printType(v.Elem) + ")"
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
	case *lang.ObjectLit:
		fields := make([]string, 0, len(v.Fields))
		for _, field := range v.Fields {
			fields = append(fields, printTypeField(field))
		}
		return "{ " + strings.Join(fields, ", ") + " }"
	}
	return "?"
}

func printTypeField(field *lang.Field) string {
	name := field.Key.Name
	if field.Key.Kind == lang.FieldString {
		name = "'" + field.Key.String + "'"
	}
	return name + ": " + printType(field.Value)
}

func printTypeObjectField(field *lang.TypeObjectField) string {
	if field.Type != nil {
		return printType(field.Type)
	}
	if field.Decl != nil {
		return printType(field.Decl)
	}
	return "?"
}
