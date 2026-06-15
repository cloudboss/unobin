package runner

import (
	"errors"
	"fmt"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
)

// loadConfigurations extracts the `factory.configurations:` block from
// a pre-parsed config, decodes every alias under each import, and
// returns both the decoded table (for the executor) and the raw form
// (for plan-file storage). The outer key is the import alias; the
// inner key is the configuration alias name. Values may reference the
// file's locals, which resolve at load, so the raw form is already
// concrete by the time it reaches a plan file.
// internal is accepted for callers that already compute the factory
// names, but stack entries with the same names are valid overrides.
// Whether every configuration a node selects exists is the executor's
// demand-driven check, not enforced here. path is preserved only for
// error messages.
func loadConfigurations(
	f *lang.File,
	path string,
	libraries map[string]*runtime.Library,
	_ map[string]map[string]bool,
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
// fails to decode. Whether a library's nodes have the configurations
// they select is checked demand-driven by the executor, so a library
// may legitimately appear here with any subset of names, or none.
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
					"configurations.%s: library declares no configuration", importAlias))
			}
			continue
		}
		if !supplied {
			continue
		}
		decodedAliases := map[string]any{}
		for aliasName, rawVal := range aliases {
			m, ok := rawVal.(map[string]any)
			if !ok {
				errs = append(errs, fmt.Errorf(
					"configurations.%s.%s: want a map, got %s",
					importAlias, aliasName, lang.TypeMessage(rawVal)))
				continue
			}
			d, err := cfg.Decode(lib.Configuration, m)
			if err != nil {
				errs = append(errs, fmt.Errorf(
					"configurations.%s.%s: %w", importAlias, aliasName, err))
				continue
			}
			decodedAliases[aliasName] = d
		}
		out[importAlias] = decodedAliases
	}
	for importAlias := range rawByImport {
		if _, known := libraries[importAlias]; !known {
			errs = append(errs, fmt.Errorf(
				"configurations.%s: unknown import alias", importAlias))
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

// readConfigurationsBlock walks the `configurations:` body and pulls
// every dotted alias.name entry into a raw form ready for decoding.
// The outer key of the result is the import alias; the inner key is
// the configuration name; the value is the raw map of fields,
// evaluated against ctx, which binds the file's locals.
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
				"%s: factory.configurations entries must be keyed by a dotted alias.name path",
				configPath))
			continue
		}
		importAlias, name := fld.Key.Path[0], fld.Key.Path[1]
		val, err := runtime.Eval(fld.Value, ctx)
		if err != nil {
			errs = append(errs, fmt.Errorf(
				"%s: factory.configurations.%s.%s: %w", configPath, importAlias, name, err))
			continue
		}
		m, ok := val.(map[string]any)
		if !ok {
			errs = append(errs, fmt.Errorf(
				"%s: factory.configurations.%s.%s must be a map", configPath, importAlias, name))
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
