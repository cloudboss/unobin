package runtime

import (
	"errors"
	"fmt"
	"slices"
)

// CheckLibraryConfigs reports Go leaves whose import alias needs a config but
// has no library-configs entry in its scope.
func (e *Executor) CheckLibraryConfigs() error {
	if e == nil || e.DAG == nil {
		return nil
	}
	addrs := make([]string, 0, len(e.DAG.Nodes))
	for addr := range e.DAG.Nodes {
		addrs = append(addrs, addr)
	}
	slices.Sort(addrs)
	var errs []error
	for _, addr := range addrs {
		n := e.DAG.Nodes[addr]
		if n == nil || n.IsComposite() {
			continue
		}
		switch n.Kind {
		case NodeResource, NodeDataSource, NodeAction:
		default:
			continue
		}
		if _, ok := libraryConfigNode(e.DAG.Nodes, n.Composite, n.Alias); ok {
			continue
		}
		lib := e.librariesFor(n)[n.Alias]
		if lib == nil || lib.Configuration == nil || lib.Configuration.Empty() {
			continue
		}
		errs = append(errs, fmt.Errorf(
			"%s: library %q requires library-configs.%s", n.Address, n.Alias, n.Alias))
	}
	return errors.Join(errs...)
}
