package runtime

import (
	"fmt"
	"maps"
	"slices"

	"github.com/cloudboss/unobin/pkg/sdk/state"
)

func (e *Executor) validatePlanEvaluationV2ResourceBindings(snapshot *state.SnapshotV2) error {
	for _, entry := range snapshot.Entries {
		if entry.Kind != state.StateResource {
			continue
		}
		binding := entry.Payload.Resource.Target.Binding
		if _, _, err := e.LibraryCatalog.resource(binding); err != nil {
			return fmt.Errorf("%s: prior resource: %w", entry.Address, err)
		}
	}
	for _, address := range slices.Sorted(maps.Keys(e.DAG.Nodes)) {
		node := e.DAG.Nodes[address]
		if node.Kind != NodeResource || node.IsComposite() {
			continue
		}
		library := e.librariesFor(node)[node.Alias]
		if library == nil {
			return fmt.Errorf("%s: library %q is not imported", address, node.Alias)
		}
		binding := Binding{LibraryPath: library.LibraryPath, Export: node.Type}
		if node.LibraryPath != "" && node.LibraryPath != binding.LibraryPath {
			return fmt.Errorf("%s: resource library path does not match import", address)
		}
		if _, _, err := e.LibraryCatalog.resource(binding); err != nil {
			return fmt.Errorf("%s: desired resource: %w", address, err)
		}
	}
	return nil
}
