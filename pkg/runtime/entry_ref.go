package runtime

import (
	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/cloudboss/unobin/pkg/stateref"
)

type EntryRef = stateref.EntryRef

func ParseEntryRef(s string) (EntryRef, error) {
	return stateref.Parse(s)
}

func EntryRefFromEntry(e *state.Entry) (EntryRef, bool) {
	if e == nil || e.Selector == nil || e.Selector.Alias == "" || e.Selector.Export == "" {
		return EntryRef{}, false
	}
	if err := stateref.ValidateAddress(e.Address); err != nil {
		return EntryRef{}, false
	}
	return EntryRef{
		Selector: state.Selector{Alias: e.Selector.Alias, Export: e.Selector.Export},
		Address:  e.Address,
	}, true
}

func EntryRefFromNode(n *Node) (EntryRef, bool) {
	if n == nil || n.Alias == "" || n.Type == "" {
		return EntryRef{}, false
	}
	if err := stateref.ValidateAddress(n.Address); err != nil {
		return EntryRef{}, false
	}
	return EntryRef{
		Selector: state.Selector{Alias: n.Alias, Export: n.Type},
		Address:  n.Address,
	}, true
}

func SameEntryRef(a, b EntryRef) bool {
	return stateref.Same(a, b)
}
