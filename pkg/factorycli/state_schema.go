package factorycli

import (
	"fmt"
	"io"
	"maps"
	"slices"

	"github.com/spf13/cobra"

	"github.com/cloudboss/unobin/internal/cmdout"
	"github.com/cloudboss/unobin/pkg/diagnostic"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
)

type stateSchemaField struct {
	Name        string             `json:"name" ub:"name"`
	Type        string             `json:"type" ub:"type"`
	Optional    bool               `json:"optional" ub:"optional"`
	Description string             `json:"description" ub:"description"`
	Fields      []stateSchemaField `json:"fields" ub:"fields"`
}

type stateSchemaType struct {
	Name          string             `json:"name" ub:"name"`
	Description   string             `json:"description" ub:"description"`
	Configuration []stateSchemaField `json:"configuration" ub:"configuration"`
}

type stateSchemaResult struct {
	Kind          string                  `json:"kind" ub:"kind"`
	FormatVersion int                     `json:"format-version" ub:"format-version"`
	Factory       factoryIdentity         `json:"factory" ub:"factory"`
	Backends      []stateSchemaType       `json:"backends" ub:"backends"`
	Encrypters    []stateSchemaType       `json:"encrypters" ub:"encrypters"`
	Diagnostics   []diagnostic.Diagnostic `json:"diagnostics" ub:"diagnostics"`
}

func newStateSchemaCmd(info Info) *cobra.Command {
	command := &cobra.Command{
		Use: "state", Short: "Print available state backends and encryption types",
		Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return doStateSchema(command, info)
		},
	}
	ownStartupCheck(command)
	addStandardFormatFlag(command)
	return command
}

func doStateSchema(command *cobra.Command, info Info) error {
	format, collector, err := beginCommandResult(command, info)
	if err != nil {
		return err
	}
	registered, err := info.registered()
	if err != nil {
		return commandResultFailure(command, format, collector.Diagnostics(), err)
	}
	result := stateSchemaResult{
		Kind: "state-schema", FormatVersion: 1, Factory: factoryIdentityFor(info),
		Backends: []stateSchemaType{}, Encrypters: []stateSchemaType{},
		Diagnostics: collector.Diagnostics(),
	}
	for _, name := range slices.Sorted(maps.Keys(registered.backends)) {
		backend := registered.backends[name]
		result.Backends = append(result.Backends, stateSchemaType{
			Name: name, Description: backend.Description,
			Configuration: stateSchemaFields(cfg.Describe(backend.Configuration)),
		})
	}
	for _, name := range slices.Sorted(maps.Keys(registered.encrypters)) {
		encrypter := registered.encrypters[name]
		result.Encrypters = append(result.Encrypters, stateSchemaType{
			Name: name, Description: encrypter.Description,
			Configuration: stateSchemaFields(cfg.Describe(encrypter.Configuration)),
		})
	}
	if format != cmdout.FormatText {
		return cmdout.WriteDocument(command.OutOrStdout(), format, result)
	}
	return writeStateSchemaText(command.OutOrStdout(), result)
}

func stateSchemaFields(fields []cfg.Field) []stateSchemaField {
	result := make([]stateSchemaField, 0, len(fields))
	for _, field := range fields {
		result = append(result, stateSchemaField{
			Name: field.Name, Type: field.Type, Optional: field.Optional,
			Description: field.Description, Fields: stateSchemaFields(field.Fields),
		})
	}
	return result
}

func writeStateSchemaText(writer io.Writer, result stateSchemaResult) error {
	for i, group := range []struct {
		name  string
		types []stateSchemaType
	}{
		{name: "State backends", types: result.Backends},
		{name: "Encryption types", types: result.Encrypters},
	} {
		if i > 0 {
			if _, err := fmt.Fprintln(writer); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(writer, group.name+":"); err != nil {
			return err
		}
		if len(group.types) == 0 {
			if _, err := fmt.Fprintln(writer, "  None."); err != nil {
				return err
			}
		}
		for _, registered := range group.types {
			if _, err := fmt.Fprintf(writer, "  %s: %s\n", registered.Name,
				registered.Description); err != nil {
				return err
			}
			if err := writeStateSchemaFields(writer, registered.Configuration, "    "); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeStateSchemaFields(writer io.Writer, fields []stateSchemaField, indent string) error {
	for _, field := range fields {
		optional := ""
		if field.Optional {
			optional = " (optional)"
		}
		if _, err := fmt.Fprintf(writer, "%s%s: %s%s\n", indent, field.Name,
			field.Type, optional); err != nil {
			return err
		}
		if field.Description != "" {
			if _, err := fmt.Fprintf(writer, "%s  %s\n", indent, field.Description); err != nil {
				return err
			}
		}
		if err := writeStateSchemaFields(writer, field.Fields, indent+"  "); err != nil {
			return err
		}
	}
	return nil
}
