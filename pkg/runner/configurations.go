package runner

import (
	"errors"
	"fmt"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
)

// loadConfigurations extracts the `stack.factory.configurations:` block from
// a pre-parsed stack, decodes every configuration body, and returns both the
// decoded table (for the executor) and the raw form (for plan-file storage).
// The outer key is the import alias; the inner key is the configuration name.
// Values may reference the file's locals, which resolve at load, so the raw
// form is already concrete by the time it reaches a plan file.
// allowed names the configuration entries the stack may provide. Named
// entries come from factory declarations; default entries may come from
// factory declarations or configurable node usage. A nil map skips this
// check for lower-level tests that do not have a factory DAG.
func loadConfigurations(
	f *lang.File,
	path string,
	libraries map[string]*runtime.Library,
	allowed map[string]map[string]bool,
) (decoded, raw map[string]map[string]any, err error) {
	rawByImport := map[string]map[string]any{}

	block, err := factoryChildObject(f, path, "configurations")
	if err != nil {
		return nil, nil, err
	}
	if block != nil {
		loaded, err := readConfigurationsBlock(path, block, runtime.NewEvalContext(f))
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
	rawByImport map[string]map[string]any,
	allowed map[string]map[string]bool,
) error {
	if allowed == nil {
		return nil
	}
	var errs []error
	for alias, names := range rawByImport {
		for name := range names {
			if !allowed[alias][name] {
				errs = append(errs, fmt.Errorf(
					"%s: %s is not declared by the factory",
					path, configurationLabel(alias, name)))
			}
		}
	}
	return errors.Join(errs...)
}

func decodeConfigurations(
	rawByImport map[string]map[string]any,
	libraries map[string]*runtime.Library,
) (map[string]map[string]any, error) {
	out := map[string]map[string]any{}
	var errs []error
	for importAlias, lib := range libraries {
		aliases, supplied := rawByImport[importAlias]
		if lib.Configuration == nil {
			if supplied {
				errs = append(errs, fmt.Errorf(
					"default configuration for %s: library declares no configuration",
					importAlias))
			}
			continue
		}
		if !supplied {
			continue
		}
		decodedAliases := map[string]any{}
		for aliasName, rawVal := range aliases {
			label := configurationLabel(importAlias, aliasName)
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
			decodedAliases[aliasName] = d
		}
		out[importAlias] = decodedAliases
	}
	for importAlias := range rawByImport {
		if _, known := libraries[importAlias]; !known {
			errs = append(errs, fmt.Errorf(
				"default configuration for %s: unknown import alias", importAlias))
		}
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return out, nil
}

// decodeConfigurationsFromPlan re-decodes the raw configurations
// stored in a plan file. The shape matches what loadConfigurations
// returns for the raw form.
func decodeConfigurationsFromPlan(
	raw map[string]map[string]any,
	libraries map[string]*runtime.Library,
) (map[string]map[string]any, error) {
	return decodeConfigurations(raw, libraries)
}

// readConfigurationsBlock walks the `configurations:` body and pulls every
// selector-body entry into a raw form ready for decoding. The outer key of the
// result is the import alias; the inner key is the configuration name; the
// value is the raw map of fields, evaluated against ctx, which binds the file's
// locals.
func readConfigurationsBlock(
	configPath string,
	block *lang.ObjectLit,
	ctx *runtime.EvalContext,
) (map[string]map[string]any, error) {
	out := map[string]map[string]any{}
	var errs []error
	for _, fld := range block.Fields {
		if fld.Key.Kind != lang.FieldPath || len(fld.Key.Path) != 2 {
			errs = append(errs, fmt.Errorf(
				"%s: configuration entries must use selector bodies like `aws {}` "+
					"or `east: aws {}`", configPath))
			continue
		}
		importAlias, name := fld.Key.Path[0], fld.Key.Path[1]
		label := configurationLabel(importAlias, name)
		val, err := runtime.Eval(fld.Value, ctx)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %s: %w", configPath, label, err))
			continue
		}
		m, ok := val.(map[string]any)
		if !ok {
			errs = append(errs, fmt.Errorf("%s: %s must be a map", configPath, label))
			continue
		}
		if out[importAlias] == nil {
			out[importAlias] = map[string]any{}
		}
		out[importAlias][name] = m
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return out, nil
}
