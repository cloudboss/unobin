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
// file, decodes the `default` alias of each import, and returns the
// table keyed by import alias and alias name. V1 reads only the
// `default` entry per import; `@module:`-driven alias selection is
// not yet wired up. A module that declares a Configuration must have
// a corresponding entry in config.ub or the load errors.
func loadConfigurations(
	configPath string,
	modules map[string]*runtime.Module,
) (map[string]map[string]any, error) {
	rawByImport := map[string]map[string]any{}

	if configPath != "" {
		src, err := os.ReadFile(configPath)
		if err != nil {
			return nil, err
		}
		f, err := lang.ParseSource(configPath, src)
		if err != nil {
			return nil, err
		}
		f.Kind = lang.FileConfig
		if errs := lang.ValidateFile(f); errs.Len() > 0 {
			return nil, errs.Err()
		}
		block := topLevelObject(f, "configurations")
		if block != nil {
			loaded, err := readConfigurationsBlock(configPath, block)
			if err != nil {
				return nil, err
			}
			rawByImport = loaded
		}
	}

	out := map[string]map[string]any{}
	var errs []string
	for alias, mod := range modules {
		if mod.Configuration == nil {
			if _, supplied := rawByImport[alias]; supplied {
				errs = append(errs, fmt.Sprintf(
					"configurations.%s: module declares no configuration", alias))
			}
			continue
		}
		raw, supplied := rawByImport[alias]
		if !supplied {
			errs = append(errs, fmt.Sprintf(
				"configurations.%s: module requires a configuration but none was given",
				alias))
			continue
		}
		decoded, err := cfg.Decode(mod.Configuration, raw)
		if err != nil {
			errs = append(errs, fmt.Sprintf("configurations.%s.default: %s", alias, err))
			continue
		}
		out[alias] = map[string]any{"default": decoded}
	}
	for alias := range rawByImport {
		if _, known := modules[alias]; !known {
			errs = append(errs, fmt.Sprintf(
				"configurations.%s: unknown import alias", alias))
		}
	}
	if len(errs) > 0 {
		return nil, errors.New(strings.Join(errs, "; "))
	}
	return out, nil
}

// readConfigurationsBlock walks the `configurations:` body and pulls
// the `default` entry under each import alias into a raw map ready
// for cfg.Decode. Anything that isn't a `default` entry is ignored
// in V1; future alias support will read additional entries.
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
		alias := fld.Key.Name
		obj, ok := fld.Value.(*lang.ObjectLit)
		if !ok {
			errs = append(errs, fmt.Sprintf(
				"%s: configurations.%s must be an object", configPath, alias))
			continue
		}
		var raw map[string]any
		for _, aliasFld := range obj.Fields {
			if aliasFld.Key.Kind != lang.FieldIdent || aliasFld.Key.Name != "default" {
				continue
			}
			val, err := runtime.Eval(aliasFld.Value, &runtime.EvalContext{})
			if err != nil {
				errs = append(errs, fmt.Sprintf(
					"%s: configurations.%s.default: %s", configPath, alias, err))
				break
			}
			m, ok := val.(map[string]any)
			if !ok {
				errs = append(errs, fmt.Sprintf(
					"%s: configurations.%s.default must be a map",
					configPath, alias))
				break
			}
			raw = m
			break
		}
		if raw == nil {
			errs = append(errs, fmt.Sprintf(
				"%s: configurations.%s: missing `default` entry", configPath, alias))
			continue
		}
		out[alias] = raw
	}
	if len(errs) > 0 {
		return nil, errors.New(strings.Join(errs, "; "))
	}
	return out, nil
}
