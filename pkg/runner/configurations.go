package runner

import (
	"errors"
	"fmt"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
)

// loadConfigurations extracts the `stack.factory.configurations:` entries
// from a pre-parsed stack, decodes every configuration body, and returns both
// the decoded table (for the executor) and the raw form (for plan-file
// storage). Tables are keyed by selector and configuration name. Values may
// reference the file's locals, which resolve at load, so the raw form is
// already concrete by the time it reaches a plan file.
// allowed names the configuration entries the stack may provide. Named entries
// come from factory declarations; default entries may come from factory
// declarations or configurable node usage. A nil map skips this check for
// lower-level tests that do not have a factory DAG.
func loadConfigurations(
	config *parsedConfig,
	path string,
	libraries map[string]*runtime.Library,
	allowed map[string]map[string]bool,
) (decoded, raw runtime.ConfigTable, err error) {
	rawByImport := runtime.ConfigTable{}
	stack := configStack(config)
	if stack != nil && stack.Factory != nil && len(stack.Factory.Configurations) > 0 {
		loaded, err := readConfigurationValues(
			path, stack.Factory.Configurations, configEvalContext(config))
		if err != nil {
			return nil, nil, err
		}
		rawByImport = loaded
	}
	if err := validateConfigurationOverrides(path, rawByImport, allowed); err != nil {
		return nil, nil, err
	}

	decoded, err = decodeConfigurations(rawByImport, libraries)
	if err != nil {
		return nil, nil, err
	}
	if len(rawByImport) > 0 {
		raw = rawByImport
	}
	return decoded, raw, nil
}

// decodeConfigurations runs cfg.Decode for each configuration alias
// under each library. It errors when an alias targets a library that
// has no Configuration, when an import is unknown, or when an entry
// fails to decode. The caller has already checked whether the stack may
// provide those configuration names.
func allowedConfigurationOverrides(
	dag *runtime.DAG,
	libraries map[string]*runtime.Library,
	internal map[string]map[string]bool,
) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	mergeConfigurationNames(out, internal)
	if dag != nil {
		mergeConfigurationNames(out, dag.ConfigurationSelections(libraries))
	}
	return out
}

func mergeConfigurationNames(dst, src map[string]map[string]bool) {
	for alias, names := range src {
		if dst[alias] == nil {
			dst[alias] = map[string]bool{}
		}
		for name, ok := range names {
			if ok {
				dst[alias][name] = true
			}
		}
	}
}

func configurationLabel(alias, name string) string {
	if name == "default" {
		return fmt.Sprintf("default configuration for %s", alias)
	}
	return "configuration." + name
}

func validateConfigurationOverrides(
	path string,
	rawByImport runtime.ConfigTable,
	allowed map[string]map[string]bool,
) error {
	if allowed == nil {
		return nil
	}
	var errs []error
	for ref := range rawByImport {
		if !allowed[ref.Alias][ref.Name] {
			errs = append(errs, fmt.Errorf(
				"%s: %s is not declared by the factory",
				path, configurationLabel(ref.Alias, ref.Name)))
		}
	}
	return errors.Join(errs...)
}

func decodeConfigurations(
	rawByImport runtime.ConfigTable,
	libraries map[string]*runtime.Library,
) (runtime.ConfigTable, error) {
	out := runtime.ConfigTable{}
	var errs []error
	for ref, rawVal := range rawByImport {
		label := configurationLabel(ref.Alias, ref.Name)
		lib, known := libraries[ref.Alias]
		if !known {
			errs = append(errs, fmt.Errorf("%s: unknown import alias", label))
			continue
		}
		if lib.Configuration == nil {
			errs = append(errs, fmt.Errorf("%s: library declares no configuration", label))
			continue
		}
		m, ok := rawVal.(map[string]any)
		if !ok {
			errs = append(errs, fmt.Errorf(
				"%s: want a map, got %s", label, lang.TypeMessage(rawVal)))
			continue
		}
		d, err := cfg.Decode(lib.Configuration, m)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", label, err))
			continue
		}
		out[ref] = d
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return out, nil
}

// decodeConfigurationsFromPlan re-decodes the raw configurations
// stored in a plan file. The form matches what loadConfigurations
// returns for the raw data.
func decodeConfigurationsFromPlan(
	raw runtime.ConfigTable,
	libraries map[string]*runtime.Library,
) (runtime.ConfigTable, error) {
	return decodeConfigurations(raw, libraries)
}

// readConfigurationValues pulls every selector-body stack configuration entry
// into a raw form ready for decoding. The key is the selector and
// configuration name; the value is the raw map of fields, evaluated against
// ctx, which binds the file's locals.
func readConfigurationValues(
	configPath string,
	values []syntax.ConfigurationValue,
	ctx *runtime.EvalContext,
) (runtime.ConfigTable, error) {
	out := runtime.ConfigTable{}
	var errs []error
	for _, value := range values {
		importAlias, name := configurationValueName(value)
		label := configurationLabel(importAlias, name)
		val, err := runtime.Eval(configurationValueExpr(value), ctx)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %s: %w", configPath, label, err))
			continue
		}
		m, ok := val.(map[string]any)
		if !ok {
			errs = append(errs, fmt.Errorf("%s: %s must be a map", configPath, label))
			continue
		}
		out[runtime.ConfigRef{Alias: importAlias, Name: name}] = m
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return out, nil
}

func configurationValueName(value syntax.ConfigurationValue) (alias, name string) {
	name = "default"
	if value.Name != nil {
		name = value.Name.Name
	}
	return value.Selector.Name, name
}

func configurationValueExpr(value syntax.ConfigurationValue) lang.Expr {
	if value.Value != nil {
		return value.Value
	}
	return value.Body
}
