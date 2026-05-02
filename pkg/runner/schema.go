package runner

import (
	"fmt"
	"strings"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/spf13/cobra"
)

func newSchemaCmd(info Info) *cobra.Command {
	return &cobra.Command{
		Use:   "schema",
		Short: "Print the stack's input declarations",
		RunE: func(cmd *cobra.Command, args []string) error {
			return doSchema(cmd, info)
		},
	}
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
