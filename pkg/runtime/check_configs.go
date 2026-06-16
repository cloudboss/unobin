package runtime

import (
	"errors"
	"fmt"
	"slices"

	"github.com/cloudboss/unobin/pkg/lang"
)

// CheckConfigurations walks the DAG and reports every
// `@configuration:` or `@configurations:` reference that cannot
// resolve against the Executor's decoded configurations. It is
// called at the start of Plan and Refresh so a typo or stale
// reference fails fast instead of reaching a CRUD call with a nil
// cfg.
func (e *Executor) CheckConfigurations() error {
	var errs []error
	addrs := make([]string, 0, len(e.DAG.Nodes))
	for a := range e.DAG.Nodes {
		addrs = append(addrs, a)
	}
	slices.Sort(addrs)
	for _, addr := range addrs {
		n := e.DAG.Nodes[addr]
		errs = append(errs, e.checkLeafConfiguration(n)...)
		errs = append(errs, e.checkCompositeRemap(n)...)
		errs = append(errs, e.checkConfigurationBodyRefs(n)...)
	}
	return errors.Join(errs...)
}

// checkConfigurationBodyRefs reports every configuration reference in
// an internal configuration's body that cannot be served: one naming
// a configuration the factory itself defines, or one the operator did
// not supply.
func (e *Executor) checkConfigurationBodyRefs(n *Node) []error {
	if n.Kind != NodeConfiguration {
		return nil
	}
	var errs []error
	lang.Walk(n.Body, func(expr lang.Expr) {
		dp, ok := expr.(*lang.DotPath)
		if !ok || dp.Root == nil || dp.Root.Name != "configuration" {
			return
		}
		if len(dp.Segments) < 2 || dp.Segments[0].Name == "" || dp.Segments[1].Name == "" {
			errs = append(errs, fmt.Errorf(
				"%s: a configuration reference has the form configuration.<name>",
				n.Address))
			return
		}
		alias, name := dp.Segments[0].Name, dp.Segments[1].Name
		if _, internal := configurationNodeAddress(e.DAG.Nodes, alias, name); internal {
			errs = append(errs, fmt.Errorf(
				"%s: references configuration.%s, which this factory defines; "+
					"only operator-supplied configurations are referenceable",
				n.Address, name))
			return
		}
		if _, ok := e.RawConfigurations[alias][name]; !ok {
			errs = append(errs, fmt.Errorf(
				"%s: references configuration.%s, which is not supplied",
				n.Address, name))
		}
	})
	return errs
}

func (e *Executor) checkLeafConfiguration(n *Node) []error {
	if n.Kind != NodeResource && n.Kind != NodeData && n.Kind != NodeAction {
		return nil
	}
	lib, ok := e.Libraries[n.Alias]
	if !ok {
		return nil
	}
	if lib.Configuration == nil {
		if n.Configuration != "" {
			return []error{fmt.Errorf(
				"%s: @configuration configuration.%s: library declares no configuration",
				n.Address, n.Configuration)}
		}
		return nil
	}
	alias, configuration := e.resolvedConfigRef(n)
	if e.configurationDeclared(alias, configuration) {
		return nil
	}
	if n.Configuration != "" {
		return []error{fmt.Errorf(
			"%s: @configuration configuration.%s: configuration not declared",
			n.Address, n.Configuration)}
	}
	return []error{fmt.Errorf(
		"%s: library %q requires a configuration; add %s { ... } under "+
			"stack.factory.configurations or configurations in the factory",
		n.Address, n.Alias, alias)}
}

// configurationDeclared reports whether a configuration name resolves
// for an alias: either the operator supplied it in the stack file or
// the factory defines it internally.
func (e *Executor) configurationDeclared(alias, name string) bool {
	if _, ok := e.Configurations[alias][name]; ok {
		return true
	}
	_, ok := configurationNodeAddress(e.DAG.Nodes, alias, name)
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
	slices.Sort(keys)
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
				"%s: @configurations.%s: configuration.%s not declared",
				n.Address, innerAlias, ref.Configuration))
		}
	}
	return errs
}
