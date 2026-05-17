package runtime

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// checkConfigurations walks the DAG and reports every
// `@configuration:` or `@configurations:` reference that cannot
// resolve against the Executor's decoded configurations. It is
// called at the start of Plan and Refresh so a typo or stale
// reference fails fast instead of reaching a CRUD call with a nil
// cfg.
func (e *Executor) checkConfigurations() error {
	var errs []string
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
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func (e *Executor) checkLeafConfiguration(n *Node) []string {
	if n.Kind != NodeResource && n.Kind != NodeData && n.Kind != NodeAction {
		return nil
	}
	if n.ConfigurationAlias == "" {
		return nil
	}
	mod, ok := e.Modules[n.NS]
	if !ok {
		return nil
	}
	if mod.Configuration == nil {
		return []string{fmt.Sprintf(
			"%s: @configuration %s.%s: module declares no configuration",
			n.Address, n.NS, n.ConfigurationAlias)}
	}
	if _, ok := e.Configurations[n.NS][n.ConfigurationAlias]; !ok {
		return []string{fmt.Sprintf(
			"%s: @configuration %s.%s: alias not declared in configurations",
			n.Address, n.NS, n.ConfigurationAlias)}
	}
	return nil
}

func (e *Executor) checkCompositeRemap(n *Node) []string {
	if n.Kind != NodeComposite || len(n.ConfigurationsRemap) == 0 {
		return nil
	}
	keys := make([]string, 0, len(n.ConfigurationsRemap))
	for k := range n.ConfigurationsRemap {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var errs []string
	for _, innerNS := range keys {
		ref := n.ConfigurationsRemap[innerNS]
		if ref.NS != innerNS {
			errs = append(errs, fmt.Sprintf(
				"%s: @configurations.%s: right-hand side import %q must match the key",
				n.Address, innerNS, ref.NS))
			continue
		}
		if _, ok := e.Configurations[ref.NS][ref.Alias]; !ok {
			errs = append(errs, fmt.Sprintf(
				"%s: @configurations.%s: alias %s.%s not declared in configurations",
				n.Address, innerNS, ref.NS, ref.Alias))
		}
	}
	return errs
}
