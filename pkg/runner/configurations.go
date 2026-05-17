package runner

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
)

// loadConfigurations reads the `configurations:` block from a config
// file, decodes every alias under each import, and returns both the
// decoded table (for the executor) and the raw form (for plan-file
// storage). The outer key is the import alias; the inner key is the
// configuration alias name. Every module that declares a
// Configuration must have at least a `default` entry in config.ub.
func loadConfigurations(
	configPath string,
	modules map[string]*runtime.Module,
) (decoded, raw map[string]map[string]any, err error) {
	rawByImport := map[string]map[string]any{}

	if configPath != "" {
		src, err := os.ReadFile(configPath)
		if err != nil {
			return nil, nil, err
		}
		f, err := lang.ParseSource(configPath, src)
		if err != nil {
			return nil, nil, err
		}
		f.Kind = lang.FileConfig
		if errs := lang.ValidateFile(f); errs.Len() > 0 {
			return nil, nil, errs.Err()
		}
		block := topLevelObject(f, "configurations")
		if block != nil {
			loaded, err := readConfigurationsBlock(configPath, block)
			if err != nil {
				return nil, nil, err
			}
			rawByImport = loaded
		}
	}

	decoded, err = decodeConfigurations(rawByImport, modules)
	if err != nil {
		return nil, nil, err
	}
	if len(rawByImport) > 0 {
		raw = rawByImport
	}
	return decoded, raw, nil
}

// decodeConfigurations runs cfg.Decode for each configuration alias
// under each module. It errors when a module requires configuration
// but none was given, when an alias targets a module that has no
// Configuration, when an import is unknown, or when the `default`
// entry is missing for a module that needs one.
func decodeConfigurations(
	rawByImport map[string]map[string]any,
	modules map[string]*runtime.Module,
) (map[string]map[string]any, error) {
	out := map[string]map[string]any{}
	var errs []string
	for importAlias, mod := range modules {
		if mod.Configuration == nil {
			if _, supplied := rawByImport[importAlias]; supplied {
				errs = append(errs, fmt.Sprintf(
					"configurations.%s: module declares no configuration", importAlias))
			}
			continue
		}
		aliases, supplied := rawByImport[importAlias]
		if !supplied || len(aliases) == 0 {
			errs = append(errs, fmt.Sprintf(
				"configurations.%s: module requires a configuration but none was given",
				importAlias))
			continue
		}
		if _, hasDefault := aliases["default"]; !hasDefault {
			errs = append(errs, fmt.Sprintf(
				"configurations.%s: missing `default` entry", importAlias))
			continue
		}
		decodedAliases := map[string]any{}
		for aliasName, rawVal := range aliases {
			m, ok := rawVal.(map[string]any)
			if !ok {
				errs = append(errs, fmt.Sprintf(
					"configurations.%s.%s: want a map, got %s",
					importAlias, aliasName, lang.TypeMessage(rawVal)))
				continue
			}
			d, err := cfg.Decode(mod.Configuration, m)
			if err != nil {
				errs = append(errs, fmt.Sprintf(
					"configurations.%s.%s: %s", importAlias, aliasName, err))
				continue
			}
			decodedAliases[aliasName] = d
		}
		out[importAlias] = decodedAliases
	}
	for importAlias := range rawByImport {
		if _, known := modules[importAlias]; !known {
			errs = append(errs, fmt.Sprintf(
				"configurations.%s: unknown import alias", importAlias))
		}
	}
	if len(errs) > 0 {
		return nil, errors.New(strings.Join(errs, "; "))
	}
	return out, nil
}

// decodeConfigurationsFromPlan re-decodes the raw configurations
// stored in a plan file. The shape matches what loadConfigurations
// returns for the raw form.
func decodeConfigurationsFromPlan(
	raw map[string]map[string]any,
	modules map[string]*runtime.Module,
) (map[string]map[string]any, error) {
	return decodeConfigurations(raw, modules)
}

// readConfigurationsBlock walks the `configurations:` body and pulls
// every alias entry under each import into a raw form ready for
// decoding. The outer key is the import alias; the inner key is the
// configuration alias name; the value is the raw map of fields.
func readConfigurationsBlock(
	configPath string,
	block *lang.ObjectLit,
) (map[string]map[string]any, error) {
	out := map[string]map[string]any{}
	var errs []string
	for _, fld := range block.Fields {
		if fld.Key.Kind != lang.FieldIdent {
			errs = append(errs, fmt.Sprintf(
				"%s: configurations key must be an identifier", configPath))
			continue
		}
		importAlias := fld.Key.Name
		obj, ok := fld.Value.(*lang.ObjectLit)
		if !ok {
			errs = append(errs, fmt.Sprintf(
				"%s: configurations.%s must be an object", configPath, importAlias))
			continue
		}
		aliases := map[string]any{}
		for _, aliasFld := range obj.Fields {
			if aliasFld.Key.Kind != lang.FieldIdent {
				continue
			}
			aliasName := aliasFld.Key.Name
			val, err := runtime.Eval(aliasFld.Value, &runtime.EvalContext{})
			if err != nil {
				errs = append(errs, fmt.Sprintf(
					"%s: configurations.%s.%s: %s",
					configPath, importAlias, aliasName, err))
				continue
			}
			m, ok := val.(map[string]any)
			if !ok {
				errs = append(errs, fmt.Sprintf(
					"%s: configurations.%s.%s must be a map",
					configPath, importAlias, aliasName))
				continue
			}
			aliases[aliasName] = m
		}
		out[importAlias] = aliases
	}
	if len(errs) > 0 {
		return nil, errors.New(strings.Join(errs, "; "))
	}
	return out, nil
}
