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
		from, err := ParseEntryRef(decl.From.Value)
		if err != nil {
			return nil, fmt.Errorf("state-moves[%d].from: %w", i, err)
		}
		to, err := ParseEntryRef(decl.To.Value)
		if err != nil {
			return nil, fmt.Errorf("state-moves[%d].to: %w", i, err)
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

func (e *Executor) applySourceEntryMoves(rs *runState) ([]PlannedEntryMove, error) {
	specs, err := e.rootEntryMoveSpecs()
	if err != nil {
		return nil, err
	}
	if len(specs) == 0 {
		return nil, nil
	}
	next, results, err := ApplyEntryMoves(
		rs.prior,
		e.DAG,
		e.Libraries,
		specs,
		EntryMoveIdempotent,
	)
	if err != nil {
		return nil, err
	}
	if next != nil {
		rs.prior = next
	}
	planned := make([]PlannedEntryMove, 0, len(results))
	for _, result := range results {
		planned = append(planned, PlannedEntryMove{
			From: result.From.String(),
			To:   result.To.String(),
		})
	}
	return planned, nil
}

func (e *Executor) applyPlannedEntryMoves(rs *runState, moves []PlannedEntryMove) error {
	if len(moves) == 0 {
		return nil
	}
	specs := make([]EntryMoveSpec, 0, len(moves))
	for i, move := range moves {
		from, err := ParseEntryRef(move.From)
		if err != nil {
			return fmt.Errorf("state-moves[%d].from: %w", i, err)
		}
		to, err := ParseEntryRef(move.To)
		if err != nil {
			return fmt.Errorf("state-moves[%d].to: %w", i, err)
		}
		specs = append(specs, EntryMoveSpec{From: from, To: to})
	}
	next, _, err := ApplyEntryMoves(
		rs.next,
		e.DAG,
		e.Libraries,
		specs,
		EntryMoveStrict,
	)
	if err != nil {
		return err
	}
	if next != nil {
		rs.next = next
		rs.prior = cloneSnapshot(next)
	}
	return nil
}
