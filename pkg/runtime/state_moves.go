package runtime

import (
	"fmt"
	"strings"

	"github.com/cloudboss/unobin/pkg/sdk/state"
)

type EntryMoveSpec struct {
	From EntryRef
	To   EntryRef
}

type EntryMoveMode int

const (
	EntryMoveStrict EntryMoveMode = iota
	EntryMoveIdempotent
)

type EntryMoveResult struct {
	From EntryRef
	To   EntryRef
}

type normalizedEntryMove struct {
	From   EntryRef
	To     EntryRef
	Prefix bool
}

func ApplyEntryMoves(
	snap *state.Snapshot,
	dag *DAG,
	libs map[string]*Library,
	specs []EntryMoveSpec,
	mode EntryMoveMode,
) (*state.Snapshot, []EntryMoveResult, error) {
	moves, err := normalizeEntryMoveSpecs(specs)
	if err != nil {
		return nil, nil, err
	}
	if snap == nil {
		if mode == EntryMoveStrict && len(moves) > 0 {
			return nil, nil, fmt.Errorf("state move: no current state")
		}
		return nil, nil, nil
	}
	if len(moves) == 0 {
		return cloneSnapshot(snap), nil, nil
	}

	stateRefs, err := entryMoveStateRefs(snap)
	if err != nil {
		return nil, nil, err
	}
	targetNodes := make(map[string]*Node, len(moves))
	for i := range moves {
		n, err := entryMoveTargetNode(dag, moves[i].To)
		if err != nil {
			return nil, nil, err
		}
		targetNodes[moves[i].To.String()] = n
	}
	for i := range moves {
		fromEntry := stateRefs[moves[i].From.String()]
		toNode := targetNodes[moves[i].To.String()]
		moves[i].Prefix = fromEntry != nil && fromEntry.Type == state.EntryLibraryCall
		moves[i].Prefix = moves[i].Prefix || toNode.IsComposite()
	}
	if mode == EntryMoveStrict {
		for _, move := range moves {
			if stateRefs[move.From.String()] == nil {
				return nil, nil, fmt.Errorf("no entry at %s", move.From.String())
			}
		}
	}

	changes, originals, err := entryMoveChanges(snap, moves)
	if err != nil {
		return nil, nil, err
	}
	if len(changes) == 0 {
		return cloneSnapshot(snap), nil, nil
	}
	if err := checkEntryMoveConflicts(snap, changes); err != nil {
		return nil, nil, err
	}
	if err := validateEntryMoveTargets(snap, dag, changes); err != nil {
		return nil, nil, err
	}

	out := cloneSnapshot(snap)
	results := make([]EntryMoveResult, 0, len(changes))
	addressMoves := make(map[string]string, len(changes))
	for idx, ent := range out.Entries {
		to, ok := changes[idx]
		if !ok {
			continue
		}
		if err := migrateMovedEntry(ent, dag, libs, to); err != nil {
			return nil, nil, err
		}
		addressMoves[originals[idx].Address] = to.Address
		ent.Address = to.Address
		ent.Selector = &state.Selector{Alias: to.Selector.Alias, Export: to.Selector.Export}
		results = append(results, EntryMoveResult{From: originals[idx], To: to})
	}
	for _, ent := range out.Entries {
		ent.DependsOn = rewriteMovedDependsOn(ent.DependsOn, addressMoves)
	}
	if err := out.Validate(); err != nil {
		return nil, nil, err
	}
	return out, results, nil
}

func normalizeEntryMoveSpecs(specs []EntryMoveSpec) ([]normalizedEntryMove, error) {
	edges := make(map[string]EntryRef, len(specs))
	order := make([]EntryRef, 0, len(specs))
	for _, spec := range specs {
		if SameEntryRef(spec.From, spec.To) {
			return nil, fmt.Errorf("state move %s: source and destination are the same", spec.From)
		}
		key := spec.From.String()
		if _, exists := edges[key]; exists {
			return nil, fmt.Errorf("duplicate source %s", key)
		}
		edges[key] = spec.To
		order = append(order, spec.From)
	}
	out := make([]normalizedEntryMove, 0, len(order))
	for _, from := range order {
		to, err := collapseEntryMove(from, edges)
		if err != nil {
			return nil, err
		}
		out = append(out, normalizedEntryMove{From: from, To: to})
	}
	return out, nil
}

func collapseEntryMove(from EntryRef, edges map[string]EntryRef) (EntryRef, error) {
	seen := map[string]int{}
	path := []string{}
	cur := from
	for {
		key := cur.String()
		if idx, ok := seen[key]; ok {
			cycle := append(path[idx:], key)
			return EntryRef{}, fmt.Errorf("cycle: %s", strings.Join(cycle, " -> "))
		}
		next, ok := edges[key]
		if !ok {
			return cur, nil
		}
		seen[key] = len(path)
		path = append(path, key)
		cur = next
	}
}

func entryMoveStateRefs(snap *state.Snapshot) (map[string]*state.Entry, error) {
	refs := make(map[string]*state.Entry, len(snap.Entries))
	for i, ent := range snap.Entries {
		ref, ok := EntryRefFromEntry(ent)
		if !ok {
			return nil, fmt.Errorf("state entry %d is missing a complete ref", i)
		}
		refs[ref.String()] = ent
	}
	return refs, nil
}

func entryMoveChanges(
	snap *state.Snapshot,
	moves []normalizedEntryMove,
) (map[int]EntryRef, map[int]EntryRef, error) {
	exact := make(map[string]normalizedEntryMove, len(moves))
	var prefixes []normalizedEntryMove
	for _, move := range moves {
		exact[move.From.String()] = move
		if move.Prefix {
			prefixes = append(prefixes, move)
		}
	}
	changes := map[int]EntryRef{}
	originals := map[int]EntryRef{}
	for i, ent := range snap.Entries {
		from, ok := EntryRefFromEntry(ent)
		if !ok {
			return nil, nil, fmt.Errorf("state entry %d is missing a complete ref", i)
		}
		to, changed := entryMoveTargetForRef(from, exact, prefixes)
		if !changed || SameEntryRef(from, to) {
			continue
		}
		changes[i] = to
		originals[i] = from
	}
	return changes, originals, nil
}

func entryMoveTargetForRef(
	from EntryRef,
	exact map[string]normalizedEntryMove,
	prefixes []normalizedEntryMove,
) (EntryRef, bool) {
	if move, ok := exact[from.String()]; ok {
		return move.To, true
	}
	best := normalizedEntryMove{}
	bestLen := -1
	for _, move := range prefixes {
		if !entryMoveHasAddressPrefix(from.Address, move.From.Address) {
			continue
		}
		if from.Address == move.From.Address {
			continue
		}
		if len(move.From.Address) > bestLen {
			best = move
			bestLen = len(move.From.Address)
		}
	}
	if bestLen < 0 {
		return EntryRef{}, false
	}
	suffix := from.Address[len(best.From.Address):]
	return EntryRef{Selector: from.Selector, Address: best.To.Address + suffix}, true
}

func entryMoveHasAddressPrefix(address, prefix string) bool {
	return address == prefix || strings.HasPrefix(address, prefix+"/")
}

func rewriteMovedDependsOn(deps []string, addressMoves map[string]string) []string {
	if len(deps) == 0 || len(addressMoves) == 0 {
		return deps
	}
	var out []string
	for i, dep := range deps {
		rewritten := rewriteMovedAddress(dep, addressMoves)
		if out == nil && rewritten != dep {
			out = append([]string{}, deps[:i]...)
		}
		if out != nil {
			out = append(out, rewritten)
		}
	}
	if out == nil {
		return deps
	}
	return out
}

func rewriteMovedAddress(address string, addressMoves map[string]string) string {
	bestFrom := ""
	bestTo := ""
	for from, to := range addressMoves {
		if !entryMoveHasAddressPrefix(address, from) {
			continue
		}
		if len(from) > len(bestFrom) {
			bestFrom = from
			bestTo = to
		}
	}
	if bestFrom == "" {
		return address
	}
	return bestTo + address[len(bestFrom):]
}

func checkEntryMoveConflicts(snap *state.Snapshot, changes map[int]EntryRef) error {
	changed := make(map[int]bool, len(changes))
	for idx := range changes {
		changed[idx] = true
	}
	occupied := map[string]EntryRef{}
	for i, ent := range snap.Entries {
		if changed[i] {
			continue
		}
		ref, _ := EntryRefFromEntry(ent)
		occupied[ent.Address] = ref
	}
	targets := map[string]EntryRef{}
	for _, to := range changes {
		if ref, exists := occupied[to.Address]; exists {
			return fmt.Errorf("destination already exists at %s", ref.String())
		}
		if ref, exists := targets[to.Address]; exists {
			return fmt.Errorf("destination already exists at %s", ref.String())
		}
		targets[to.Address] = to
	}
	return nil
}

func validateEntryMoveTargets(
	snap *state.Snapshot,
	dag *DAG,
	changes map[int]EntryRef,
) error {
	for idx, to := range changes {
		n, err := entryMoveTargetNode(dag, to)
		if err != nil {
			return err
		}
		if err := validateEntryMoveTarget(snap.Entries[idx], n); err != nil {
			return err
		}
	}
	return nil
}

func entryMoveTargetNode(dag *DAG, ref EntryRef) (*Node, error) {
	if dag == nil {
		return nil, fmt.Errorf("state move %s: destination is not in this factory", ref.String())
	}
	n := dag.Nodes[templateAddress(ref.Address)]
	if n == nil || !entryMoveNodeMatchesRef(n, ref) {
		return nil, fmt.Errorf("state move %s: destination is not in this factory", ref.String())
	}
	return n, nil
}

func entryMoveNodeMatchesRef(n *Node, ref EntryRef) bool {
	return n != nil && n.Alias == ref.Selector.Alias && n.Type == ref.Selector.Export
}

func validateEntryMoveTarget(ent *state.Entry, n *Node) error {
	switch ent.Type {
	case state.EntryLeaf:
		if n.Kind != NodeResource || n.IsComposite() {
			return fmt.Errorf("leaf entry cannot move to %s", n.Kind)
		}
	case state.EntryData:
		if n.Kind != NodeData || n.IsComposite() {
			return fmt.Errorf("data entry cannot move to %s", n.Kind)
		}
	case state.EntryAction:
		if n.Kind != NodeAction || n.IsComposite() {
			return fmt.Errorf("action entry cannot move to %s", n.Kind)
		}
	case state.EntryLibraryCall:
		if !n.IsComposite() {
			return fmt.Errorf("library-call entry cannot move to primitive %s", n.Kind)
		}
	default:
		return fmt.Errorf("unsupported state entry kind %s", ent.Type)
	}
	return nil
}

func migrateMovedEntry(
	ent *state.Entry,
	dag *DAG,
	libs map[string]*Library,
	to EntryRef,
) error {
	if ent.Type != state.EntryLeaf {
		return nil
	}
	n, err := entryMoveTargetNode(dag, to)
	if err != nil {
		return err
	}
	lib, ok := entryMoveLibrariesForNode(n, dag, libs)[n.Alias]
	if !ok {
		return fmt.Errorf("library %q is not imported", n.Alias)
	}
	rt, ok := lib.Resources[n.Type]
	if !ok {
		return fmt.Errorf("library %s has no resource %q", n.Alias, n.Type)
	}
	migrated, err := migrateEntry(rt, n.Alias, ent.SchemaVersion,
		MigrationState{Inputs: ent.Inputs, Outputs: ent.Outputs})
	if err != nil {
		return err
	}
	ent.Inputs = migrated.Inputs
	ent.Outputs = migrated.Outputs
	ent.SchemaVersion = rt.SchemaVersion()
	return nil
}

func entryMoveLibrariesForNode(n *Node, dag *DAG, libs map[string]*Library) map[string]*Library {
	if n != nil && n.Composite != "" && dag != nil {
		if boundary := dag.Nodes[n.Composite]; boundary != nil && boundary.Libraries != nil {
			return boundary.Libraries
		}
	}
	return libs
}
