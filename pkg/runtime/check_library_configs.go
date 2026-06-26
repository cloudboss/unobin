package runtime

import (
	"errors"
	"fmt"
	"slices"
)

// MissingLibraryConfig identifies a leaf whose alias needs a config entry.
type MissingLibraryConfig struct {
	Address string
	Alias   string
}

// MissingLibraryConfigs returns leaf aliases that need a library-configs entry.
func MissingLibraryConfigs(dag *DAG, libs map[string]*Library) []MissingLibraryConfig {
	if dag == nil {
		return nil
	}
	addrs := make([]string, 0, len(dag.Nodes))
	for addr := range dag.Nodes {
		addrs = append(addrs, addr)
	}
	slices.Sort(addrs)
	missing := make([]MissingLibraryConfig, 0)
	for _, addr := range addrs {
		n := dag.Nodes[addr]
		if !nodeCanNeedLibraryConfig(n) {
			continue
		}
		if _, ok := libraryConfigNode(dag.Nodes, n.Composite, n.Alias); ok {
			continue
		}
		lib := librariesForNode(dag, libs, n)[n.Alias]
		if !libraryNeedsConfig(lib) {
			continue
		}
		missing = append(missing, MissingLibraryConfig{Address: n.Address, Alias: n.Alias})
	}
	return missing
}

func nodeCanNeedLibraryConfig(n *Node) bool {
	if n == nil || n.IsComposite() {
		return false
	}
	switch n.Kind {
	case NodeResource, NodeDataSource, NodeAction:
		return true
	default:
		return false
	}
}

func librariesForNode(dag *DAG, root map[string]*Library, n *Node) map[string]*Library {
	if n == nil || n.Composite == "" || dag == nil {
		return root
	}
	if boundary, ok := dag.Nodes[n.Composite]; ok && boundary.Libraries != nil {
		return boundary.Libraries
	}
	return root
}

func libraryNeedsConfig(lib *Library) bool {
	if lib == nil {
		return false
	}
	if lib.Schema != nil && lib.Schema.HasConfiguration {
		if lib.Schema.ConfigurationEmpty {
			return false
		}
		if lib.Schema.ConfigurationFields != nil {
			return len(lib.Schema.ConfigurationFields) > 0
		}
		if lib.Schema.Configuration != nil {
			return len(lib.Schema.Configuration) > 0
		}
		return false
	}
	return lib.Configuration != nil && !lib.Configuration.Empty()
}

// CheckLibraryConfigs reports Go leaves whose import alias needs a config but
// has no library-configs entry in its scope.
func (e *Executor) CheckLibraryConfigs() error {
	if e == nil {
		return nil
	}
	missing := MissingLibraryConfigs(e.DAG, e.Libraries)
	errs := make([]error, 0, len(missing))
	for _, m := range missing {
		errs = append(errs, fmt.Errorf(
			"%s: library %q requires library-configs.%s", m.Address, m.Alias, m.Alias))
	}
	return errors.Join(errs...)
}
