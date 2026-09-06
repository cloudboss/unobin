package runtime

import (
	"fmt"
	"strings"
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
	return EntryRef{Address: best.To.Address + suffix}, true
}

func entryMoveHasAddressPrefix(address, prefix string) bool {
	return address == prefix || strings.HasPrefix(address, prefix+"/")
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
	return n != nil && templateAddress(ref.Address) == n.Address
}

func entryMoveLibrariesForNode(n *Node, dag *DAG, libs map[string]*Library) map[string]*Library {
	if n != nil && n.Composite != "" && dag != nil {
		if boundary := dag.Nodes[n.Composite]; boundary != nil && boundary.Libraries != nil {
			return boundary.Libraries
		}
	}
	return libs
}
