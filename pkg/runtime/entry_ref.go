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
	if e == nil {
		return EntryRef{}, false
	}
	if err := stateref.ValidateAddress(e.Address); err != nil {
		return EntryRef{}, false
	}
	return EntryRef{Address: e.Address}, true
}

func EntryRefFromNode(n *Node) (EntryRef, bool) {
	if n == nil {
		return EntryRef{}, false
	}
	if err := stateref.ValidateAddress(n.Address); err != nil {
		return EntryRef{}, false
	}
	return EntryRef{Address: n.Address}, true
}

func SameEntryRef(a, b EntryRef) bool {
	return stateref.Same(a, b)
}
