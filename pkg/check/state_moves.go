package check

import (
	"strings"

	"github.com/cloudboss/unobin/pkg/lang/parse"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/stateref"
)

func (c *referenceChecker) checkStateMoves() {
	if c.rootSyntax == nil {
		return
	}
	c.checkStateMoveBlock(*c.rootSyntax, c.dag)
	seen := map[*syntax.FactoryBody]bool{}
	for _, n := range c.dag.Nodes {
		if !n.IsComposite() || n.CompositeSyntaxBody == nil {
			continue
		}
		if seen[n.CompositeSyntaxBody] {
			continue
		}
		seen[n.CompositeSyntaxBody] = true
		libs := n.Libraries
		if libs == nil {
			libs = c.libraries[n.Composite]
		}
		c.checkStateMoveBlock(
			*n.CompositeSyntaxBody,
			runtime.BuildSyntaxDAG(*n.CompositeSyntaxBody, libs),
		)
	}
}

func (c *referenceChecker) checkStateMoveBlock(body syntax.FactoryBody, dag *runtime.DAG) {
	moves := stateMoveCheckSpecs(body.StateMoves)
	if len(moves) == 0 {
		return
	}
	edges := make(map[string]stateref.EntryRef, len(moves))
	for _, move := range moves {
		edges[move.from.String()] = move.to
	}
	for _, move := range moves {
		final, ok := collapseStateMoveCheck(move.from, edges)
		if !ok {
			continue
		}
		if stateMoveDestinationExists(dag, final) {
			continue
		}
		c.addf(move.pos, "state-moves[%d] %s -> %s: destination is not in this factory",
			move.index, move.from.String(), final.String())
	}
}

type stateMoveCheckSpec struct {
	index int
	from  stateref.EntryRef
	to    stateref.EntryRef
	pos   parse.Position
}

func stateMoveCheckSpecs(decls []syntax.StateMoveDecl) []stateMoveCheckSpec {
	out := make([]stateMoveCheckSpec, 0, len(decls))
	for i, decl := range decls {
		if decl.From == nil || decl.To == nil {
			continue
		}
		from, err := stateref.Parse(decl.From.Value)
		if err != nil {
			continue
		}
		to, err := stateref.Parse(decl.To.Value)
		if err != nil {
			continue
		}
		out = append(out, stateMoveCheckSpec{
			index: i,
			from:  from,
			to:    to,
			pos:   decl.From.S.Start,
		})
	}
	return out
}

func collapseStateMoveCheck(
	from stateref.EntryRef,
	edges map[string]stateref.EntryRef,
) (stateref.EntryRef, bool) {
	seen := map[string]bool{}
	cur := from
	for {
		key := cur.String()
		if seen[key] {
			return stateref.EntryRef{}, false
		}
		next, ok := edges[key]
		if !ok {
			return cur, true
		}
		seen[key] = true
		cur = next
	}
}

func stateMoveDestinationExists(dag *runtime.DAG, ref stateref.EntryRef) bool {
	if dag == nil {
		return false
	}
	n := dag.Nodes[stateMoveTemplateAddress(ref.Address)]
	if n == nil {
		return false
	}
	_, ok := runtime.EntryRefFromNode(n)
	return ok
}

func stateMoveTemplateAddress(addr string) string {
	var out strings.Builder
	rest := addr
	for {
		start := strings.Index(rest, "['")
		if start < 0 {
			out.WriteString(rest)
			return out.String()
		}
		out.WriteString(rest[:start])
		rest = rest[start:]
		end := strings.Index(rest, "']")
		if end < 0 {
			out.WriteString(rest)
			return out.String()
		}
		rest = rest[end+2:]
	}
}
