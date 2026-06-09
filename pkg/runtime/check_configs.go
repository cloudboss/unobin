package runtime

import (
	"errors"
	"fmt"
	"sort"
)

// checkConfigurations walks the DAG and reports every
// `@configuration:` or `@configurations:` reference that cannot
// resolve against the Executor's decoded configurations. It is
// called at the start of Plan and Refresh so a typo or stale
// reference fails fast instead of reaching a CRUD call with a nil
// cfg.
func (e *Executor) checkConfigurations() error {
	var errs []error
	addrs := make([]string, 0, len(e.DAG.Nodes))
	for a := range e.DAG.Nodes {
		addrs = append(addrs, a)
	}
	sort.Strings(addrs)
	for _, addr := range addrs {
		n := e.DAG.Nodes[addr]
		errs = append(errs, e.checkLeafConfiguration(n)...)
		errs = append(errs, e.checkCompositeRemap(n)...)
	}
	return errors.Join(errs...)
}

func (e *Executor) checkLeafConfiguration(n *Node) []error {
	if n.Kind != NodeResource && n.Kind != NodeData && n.Kind != NodeAction {
		return nil
	}
	if n.Configuration == "" {
		return nil
	}
	lib, ok := e.Libraries[n.Alias]
	if !ok {
		return nil
	}
	if lib.Configuration == nil {
		return []error{fmt.Errorf(
			"%s: @configuration %s.%s: library declares no configuration",
			n.Address, n.Alias, n.Configuration)}
	}
	if !e.configurationDeclared(n.Alias, n.Configuration) {
		return []error{fmt.Errorf(
			"%s: @configuration %s.%s: configuration not declared",
			n.Address, n.Alias, n.Configuration)}
	}
	return nil
}

// configurationDeclared reports whether a configuration name resolves
// for an alias: either the operator supplied it in config.ub or the
// factory defines it internally.
func (e *Executor) configurationDeclared(alias, name string) bool {
	if _, ok := e.Configurations[alias][name]; ok {
		return true
	}
	_, ok := e.DAG.Nodes[configurationAddress(alias, name)]
	return ok
}

func (e *Executor) checkCompositeRemap(n *Node) []error {
	if !n.IsComposite() || len(n.ConfigurationsRemap) == 0 {
		return nil
	}
	keys := make([]string, 0, len(n.ConfigurationsRemap))
	for k := range n.ConfigurationsRemap {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var errs []error
	for _, innerAlias := range keys {
		ref := n.ConfigurationsRemap[innerAlias]
		if ref.Alias != innerAlias {
			errs = append(errs, fmt.Errorf(
				"%s: @configurations.%s: right-hand side import %q must match the key",
				n.Address, innerAlias, ref.Alias))
			continue
		}
		if !e.configurationDeclared(ref.Alias, ref.Configuration) {
			errs = append(errs, fmt.Errorf(
				"%s: @configurations.%s: configuration %s.%s not declared",
				n.Address, innerAlias, ref.Alias, ref.Configuration))
		}
	}
	return errs
}
