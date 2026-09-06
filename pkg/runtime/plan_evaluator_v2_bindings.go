package runtime

import (
	"fmt"
	"maps"
	"slices"

	"github.com/cloudboss/unobin/pkg/sdk/state"
)

func (e *Executor) validatePlanEvaluationV2Bindings(snapshot *state.SnapshotV2) error {
	return e.validateFactoryV2Bindings(snapshot, e.Destroy)
}

func (e *Executor) validateFactoryV2Bindings(snapshot *state.SnapshotV2, destroy bool) error {
	for _, entry := range snapshot.Entries {
		if entry.Kind != state.StateResource {
			continue
		}
		binding := entry.Payload.Resource.Target.Binding
		if _, _, err := e.LibraryCatalog.resource(binding); err != nil {
			return fmt.Errorf("%s: prior resource: %w", entry.Address, err)
		}
	}
	if destroy {
		return nil
	}
	for _, address := range slices.Sorted(maps.Keys(e.DAG.Nodes)) {
		node := e.DAG.Nodes[address]
		if node.Kind == NodeOutput {
			continue
		}
		library := e.librariesFor(node)[node.Alias]
		if library == nil {
			return fmt.Errorf("%s: library %q is not imported", address, node.Alias)
		}
		if e.LibraryCatalog.libraries[library.LibraryPath] != library {
			return fmt.Errorf("%s: library %q is not in the factory catalog", address, library.LibraryPath)
		}
		binding := Binding{LibraryPath: library.LibraryPath, Export: node.Type}
		if node.LibraryPath != "" && node.LibraryPath != binding.LibraryPath {
			return fmt.Errorf("%s: %s library path does not match import", address, node.Kind)
		}
		if node.Kind != NodeLibraryConfig {
			if err := binding.Validate(); err != nil {
				return fmt.Errorf("%s: %w", address, err)
			}
		}
		if node.IsComposite() {
			if library.Composite(node.Kind, node.Type) == nil {
				return fmt.Errorf("%s: library %q has no composite %q", address, binding.LibraryPath, node.Type)
			}
			continue
		}
		var err error
		switch node.Kind {
		case NodeResource:
			_, _, err = e.LibraryCatalog.resource(binding)
		case NodeDataSource:
			if library.DataSources[node.Type] == nil {
				err = fmt.Errorf("library %q has no data source %q", binding.LibraryPath, node.Type)
			}
		case NodeAction:
			if library.Actions[node.Type] == nil {
				err = fmt.Errorf("library %q has no action %q", binding.LibraryPath, node.Type)
			}
		case NodeLibraryConfig:
			if library.Configuration == nil {
				err = fmt.Errorf("library %q declares no configuration", binding.LibraryPath)
			} else {
				_, err = resolveLibraryConfigurationDefinition(binding.LibraryPath, library)
			}
		default:
			err = fmt.Errorf("unsupported node kind %q", node.Kind)
		}
		if err != nil {
			return fmt.Errorf("%s: desired %s: %w", address, node.Kind, err)
		}
	}
	return nil
}
