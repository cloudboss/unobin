package runner

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	ufs "github.com/cloudboss/unobin/pkg/fs"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/spf13/cobra"
)

func newSchemaCmd(info Info) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "schema",
		Short: "Print the stack's input declarations",
		RunE: func(cmd *cobra.Command, args []string) error {
			return doSchema(cmd, info)
		},
	}
	var outPath string
	tmpl := &cobra.Command{
		Use:   "template",
		Short: "Print a starter config.ub for this stack",
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
	f, err := parsedFile(info)
	if err != nil {
		return err
	}
	inputs := topLevelObject(f, "inputs")
	if inputs == nil || len(inputs.Fields) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No inputs declared.")
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
		fmt.Fprintf(cmd.OutOrStdout(), "%s: %s", fld.Key.Name, typeStr)
		if description != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "  -- %s", description)
		}
		fmt.Fprintln(cmd.OutOrStdout())
	}
	return nil
}

func doSchemaTemplate(cmd *cobra.Command, info Info, outPath string) error {
	f, err := parsedFile(info)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	renderSchemaTemplate(&buf, f, info)
	if outPath == "" {
		_, err := cmd.OutOrStdout().Write(buf.Bytes())
		return err
	}
	return ufs.WriteFileAtomic(outPath, buf.Bytes(), 0o644)
}

func renderSchemaTemplate(out io.Writer, f *lang.File, info Info) {
	fmt.Fprintln(out, "stack: {")
	if info.ModulePath != "" {
		fmt.Fprintf(out, "  module-path: '%s'\n", info.ModulePath)
	}
	fmt.Fprintf(out,
		"  supported-versions: [\n    { version: '%s', commit: '%s' },\n  ]\n}\n",
		info.StackVersion, info.StackCommit)
	inputs := topLevelObject(f, "inputs")
	if inputs == nil || len(inputs.Fields) == 0 {
		return
	}
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
