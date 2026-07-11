package runner

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/cloudboss/unobin/internal/cmdout"
	"github.com/cloudboss/unobin/pkg/backends"
	"github.com/cloudboss/unobin/pkg/diagnostic"
	"github.com/cloudboss/unobin/pkg/encrypters"
	ufs "github.com/cloudboss/unobin/pkg/fs"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/spf13/cobra"
)

type schemaInput struct {
	Name        string  `json:"name"        ub:"name"`
	Type        string  `json:"type"        ub:"type"`
	Default     *string `json:"default"     ub:"default"`
	Description string  `json:"description" ub:"description"`
	Sensitive   bool    `json:"sensitive"   ub:"sensitive"`
}

type schemaOutput struct {
	Name        string `json:"name"        ub:"name"`
	Description string `json:"description" ub:"description"`
	Sensitive   bool   `json:"sensitive"   ub:"sensitive"`
}

type schemaResult struct {
	Kind          string                  `json:"kind"           ub:"kind"`
	FormatVersion int                     `json:"format-version" ub:"format-version"`
	Factory       factoryIdentity         `json:"factory"        ub:"factory"`
	Inputs        []schemaInput           `json:"inputs"         ub:"inputs"`
	Outputs       []schemaOutput          `json:"outputs"        ub:"outputs"`
	Diagnostics   []diagnostic.Diagnostic `json:"diagnostics"    ub:"diagnostics"`
}

func newSchemaCmd(info Info) (*cobra.Command, *cobra.Command) {
	cmd := &cobra.Command{
		Use:   "schema",
		Short: "Inspect the factory schema",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	show := &cobra.Command{
		Use:   "show",
		Short: "Print the factory's input declarations",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return doSchema(cmd, info)
		},
	}
	addStandardFormatFlag(show)
	var outPath string
	tmpl := &cobra.Command{
		Use:   "template",
		Short: "Print a starter stack file for this factory",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return doSchemaTemplate(cmd, info, outPath)
		},
	}
	tmpl.Flags().StringVarP(&outPath, "out", "o", "",
		"Write the template to this file instead of stdout.")
	cmd.AddCommand(show)
	cmd.AddCommand(tmpl)
	return cmd, show
}

func doSchema(cmd *cobra.Command, info Info) error {
	formatValue, err := cmd.Flags().GetString("format")
	if err != nil {
		return err
	}
	format, err := cmdout.ParseFormat(formatValue)
	if err != nil {
		return err
	}
	collector := &diagnostic.Collector{}
	if err := checkLinkedUnobin(cmd, info.UnobinVersion, format, collector); err != nil {
		return err
	}
	parsed, err := parseFactory(info)
	if err != nil {
		if format.Machine() {
			return cmdout.WriteCommandError(cmd, format, collector.Diagnostics(), err)
		}
		return err
	}
	result, err := buildSchemaResult(info, parsed, collector.Diagnostics())
	if err != nil {
		if format.Machine() {
			return cmdout.WriteCommandError(cmd, format, collector.Diagnostics(), err)
		}
		return err
	}
	if format == cmdout.FormatText {
		return writeSchemaText(cmd.OutOrStdout(), result)
	}
	return cmdout.WriteDocument(cmd.OutOrStdout(), format, result)
}

func buildSchemaResult(
	info Info,
	parsed *parsedFactory,
	diagnostics []diagnostic.Diagnostic,
) (schemaResult, error) {
	result := schemaResult{
		Kind:          "schema",
		FormatVersion: 1,
		Factory:       factoryIdentityFor(info),
		Inputs:        []schemaInput{},
		Outputs:       []schemaOutput{},
		Diagnostics:   diagnostic.Normalize(diagnostics),
	}
	sensitiveInputs := lang.SensitiveInputs(parsed.inputBlock())
	for _, input := range parsed.inputs() {
		typeText, err := lang.FormatTypeExpr(input.typeExpr)
		if err != nil {
			return schemaResult{}, err
		}
		item := schemaInput{
			Name:        input.name,
			Type:        typeText,
			Description: input.description,
			Sensitive:   sensitiveInputs[input.name],
		}
		if input.defaultExpr != nil {
			defaultText, err := lang.FormatExpr(input.defaultExpr)
			if err != nil {
				return schemaResult{}, err
			}
			item.Default = &defaultText
		}
		result.Inputs = append(result.Inputs, item)
	}
	sensitiveOutputs := rootSensitiveOutputs(parsed)
	for _, output := range parsed.outputs() {
		result.Outputs = append(result.Outputs, schemaOutput{
			Name:        output.name,
			Description: lang.OutputDescription(output.body),
			Sensitive:   sensitiveOutputs[output.name],
		})
	}
	return result, nil
}

func writeSchemaText(out io.Writer, result schemaResult) error {
	var buf strings.Builder
	if len(result.Inputs) == 0 {
		buf.WriteString("inputs: none\n")
	} else {
		buf.WriteString("inputs:\n")
		for _, input := range result.Inputs {
			fmt.Fprintf(&buf, "  %s: %s", input.Name, indentSchemaExpr(input.Type))
			if input.Default != nil {
				fmt.Fprintf(&buf, "  default: %s", indentSchemaExpr(*input.Default))
			}
			if input.Sensitive {
				buf.WriteString("  (sensitive)")
			}
			if input.Description != "" {
				fmt.Fprintf(&buf, "  -- %s", input.Description)
			}
			buf.WriteByte('\n')
		}
	}
	if len(result.Outputs) == 0 {
		buf.WriteString("outputs: none\n")
	} else {
		buf.WriteString("outputs:\n")
		for _, output := range result.Outputs {
			fmt.Fprintf(&buf, "  %s", output.Name)
			if output.Sensitive {
				buf.WriteString("  (sensitive)")
			}
			if output.Description != "" {
				fmt.Fprintf(&buf, "  -- %s", output.Description)
			}
			buf.WriteByte('\n')
		}
	}
	_, err := io.WriteString(out, buf.String())
	return err
}

func indentSchemaExpr(value string) string {
	return strings.ReplaceAll(value, "\n", "\n  ")
}

func doSchemaTemplate(cmd *cobra.Command, info Info, outPath string) error {
	parsed, err := parseFactory(info)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := renderSchemaTemplate(&buf, parsed, info); err != nil {
		return err
	}
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
func renderSchemaTemplate(out io.Writer, parsed *parsedFactory, info Info) error {
	fmt.Fprintln(out, "stack: {")
	fmt.Fprintln(out, "factory: {")
	fmt.Fprint(out, renderPinBlock(info.LibraryPath, info.FactoryVersion, info.ContentRevision))
	if err := renderInputsTemplate(out, parsed); err != nil {
		return err
	}
	fmt.Fprintln(out, "}")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "state: "+backends.LocalName+" {")
	fmt.Fprintln(out, "path: '.unobin/state'")
	fmt.Fprintln(out, "}")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "encryption: "+encrypters.NoopName+" {}")
	fmt.Fprintln(out, "}")
	return nil
}

// renderInputsTemplate scaffolds the factory.inputs block: one
// placeholder line per declared input, with its description and type
// alongside.
func renderInputsTemplate(out io.Writer, parsed *parsedFactory) error {
	inputs := parsed.inputs()
	if len(inputs) == 0 {
		return nil
	}
	fmt.Fprintln(out, "inputs: {")
	for _, input := range inputs {
		typeText, err := lang.FormatTypeExpr(input.typeExpr)
		if err != nil {
			return err
		}
		if input.description != "" {
			fmt.Fprintf(out, "# %s\n", input.description)
		}
		fmt.Fprintf(out, "%s: %s  # type: %s\n",
			input.name,
			placeholderForType(input.typeExpr),
			strings.ReplaceAll(typeText, "\n", "\n# "),
		)
	}
	fmt.Fprintln(out, "}")
	return nil
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
