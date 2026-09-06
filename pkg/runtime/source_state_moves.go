package runtime

import (
	"fmt"

	"github.com/cloudboss/unobin/pkg/lang/syntax"
)

func syntaxEntryMoveSpecs(decls []syntax.StateMoveDecl) ([]EntryMoveSpec, error) {
	specs := make([]EntryMoveSpec, 0, len(decls))
	for i, decl := range decls {
		if decl.From == nil || decl.To == nil {
			continue
		}
		from := decl.From.Ref
		if from.Address == "" {
			return nil, fmt.Errorf("state-moves[%d].from: expected state ref", i)
		}
		to := decl.To.Ref
		if to.Address == "" {
			return nil, fmt.Errorf("state-moves[%d].to: expected state ref", i)
		}
		specs = append(specs, EntryMoveSpec{From: from, To: to})
	}
	return specs, nil
}

func (e *Executor) rootEntryMoveSpecs() ([]EntryMoveSpec, error) {
	if e.SyntaxSource == nil {
		return nil, nil
	}
	return syntaxEntryMoveSpecs(e.SyntaxSource.StateMoves)
}

func prefixedEntryRef(prefix string, ref EntryRef) EntryRef {
	return EntryRef{Address: joinAddress(prefix, ref.Address)}
}
