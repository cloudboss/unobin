package runtime

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cloudboss/unobin/pkg/sdk/state"
)

// ConfigTable stores configuration values by selector and configuration name.
type ConfigTable map[ConfigRef]any

func (t ConfigTable) Lookup(ref ConfigRef) (any, bool) {
	if t == nil || ref.IsZero() {
		return nil, false
	}
	v, ok := t[ref]
	return v, ok
}

func (t ConfigTable) Has(alias, name string) bool {
	_, ok := t.Lookup(ConfigRef{Alias: alias, Name: name})
	return ok
}

func (r ConfigRef) IsZero() bool {
	return r.Alias == "" && r.Name == ""
}

func (r ConfigRef) String() string {
	if r.IsZero() {
		return ""
	}
	return r.Alias + "." + r.Name
}

func (r ConfigRef) stateRef() (*state.ConfigurationRef, error) {
	if r.IsZero() {
		return nil, nil
	}
	if r.Alias == "" || r.Name == "" || strings.Contains(r.Name, ".") {
		return nil, fmt.Errorf("configuration ref %q is invalid", r.String())
	}
	out := &state.ConfigurationRef{Selector: state.Selector{Alias: r.Alias}}
	if r.Name == "default" {
		out.Kind = "default"
		return out, nil
	}
	out.Kind = "named"
	out.Name = r.Name
	return out, nil
}

func configRefFromState(ref *state.ConfigurationRef) (ConfigRef, error) {
	if ref == nil {
		return ConfigRef{}, nil
	}
	if err := state.ValidateConfigurationRef(ref); err != nil {
		return ConfigRef{}, err
	}
	if ref.Kind == "default" {
		return ConfigRef{Alias: ref.Selector.Alias, Name: "default"}, nil
	}
	return ConfigRef{Alias: ref.Selector.Alias, Name: ref.Name}, nil
}

func configRefFromJSON(raw json.RawMessage) (ConfigRef, error) {
	ref, err := state.ParseConfigurationRefJSON(raw)
	if err != nil {
		return ConfigRef{}, err
	}
	return configRefFromState(ref)
}
