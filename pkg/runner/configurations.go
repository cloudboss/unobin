package runner

import (
	"errors"
	"fmt"
	"slices"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
)

// loadConfigurations extracts the `configurations:` block from a pre-
// parsed config, decodes every alias under each import, and returns
// both the decoded table (for the executor) and the raw form (for
// plan-file storage). The outer key is the import alias; the inner
// key is the configuration alias name. Values may reference inputs
// (var.x); inputs holds the effective values they resolve against, so
// the raw form is already concrete by the time it reaches a plan file.
// internal names the configurations the factory defines in source; an
// operator entry under one of those names is rejected, since the
// factory owns it. Whether every configuration a node selects exists
// is the executor's demand-driven check, not enforced here. path is
// preserved only for error messages.
func loadConfigurations(
	f *lang.File,
	path string,
	libraries map[string]*runtime.Library,
	inputs map[string]any,
	internal map[string]map[string]bool,
) (decoded, raw map[string]map[string]any, err error) {
	rawByImport := map[string]map[string]any{}

	if f != nil {
		block := topLevelObject(f, "configurations")
		if block != nil {
			loaded, err := readConfigurationsBlock(path, block, inputs)
			if err != nil {
				return nil, nil, err
			}
			rawByImport = loaded
		}
	}

	if err := rejectInternalNames(path, rawByImport, internal); err != nil {
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

// rejectInternalNames reports every config.ub configuration entry
// whose name the factory defines internally. The factory owns those
// names; an operator value for one would be ignored or fought over,
// so it is an error instead.
func rejectInternalNames(
	path string,
	rawByImport map[string]map[string]any,
	internal map[string]map[string]bool,
) error {
	var errs []error
	aliases := make([]string, 0, len(rawByImport))
	for alias := range rawByImport {
		aliases = append(aliases, alias)
	}
	slices.Sort(aliases)
	for _, alias := range aliases {
		names := make([]string, 0, len(rawByImport[alias]))
		for name := range rawByImport[alias] {
			names = append(names, name)
		}
		slices.Sort(names)
		for _, name := range names {
			if internal[alias][name] {
				errs = append(errs, fmt.Errorf(
					"%s: configurations.%s.%s: defined internally by the factory; "+
						"remove this entry from config.ub", path, alias, name))
			}
		}
	}
	return errors.Join(errs...)
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
// every alias entry under each import into a raw form ready for
// decoding. The outer key is the import alias; the inner key is the
// configuration alias name; the value is the raw map of fields,
// evaluated with the effective inputs in scope so var.x references
// resolve here.
func readConfigurationsBlock(
	configPath string,
	block *lang.ObjectLit,
	inputs map[string]any,
) (map[string]map[string]any, error) {
	out := map[string]map[string]any{}
	var errs []error
	for _, fld := range block.Fields {
		if fld.Key.Kind != lang.FieldIdent {
			errs = append(errs, fmt.Errorf(
				"%s: configurations key must be an identifier", configPath))
			continue
		}
		importAlias := fld.Key.Name
		obj, ok := fld.Value.(*lang.ObjectLit)
		if !ok {
			errs = append(errs, fmt.Errorf(
				"%s: configurations.%s must be an object", configPath, importAlias))
			continue
		}
		aliases := map[string]any{}
		for _, aliasFld := range obj.Fields {
			if aliasFld.Key.Kind != lang.FieldIdent {
				continue
			}
			aliasName := aliasFld.Key.Name
			val, err := runtime.Eval(aliasFld.Value, &runtime.EvalContext{Vars: inputs})
			if err != nil {
				errs = append(errs, fmt.Errorf(
					"%s: configurations.%s.%s: %w",
					configPath, importAlias, aliasName, err))
				continue
			}
			m, ok := val.(map[string]any)
			if !ok {
				errs = append(errs, fmt.Errorf(
					"%s: configurations.%s.%s must be a map",
					configPath, importAlias, aliasName))
				continue
			}
			aliases[aliasName] = m
		}
		out[importAlias] = aliases
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return out, nil
}
