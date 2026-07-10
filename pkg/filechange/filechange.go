package filechange

import (
	"cmp"
	"fmt"
	"slices"
)

type Action string

const (
	ActionCreated   Action = "created"
	ActionUpdated   Action = "updated"
	ActionRemoved   Action = "removed"
	ActionUnchanged Action = "unchanged"
)

type Change struct {
	Path   string `json:"path"   ub:"path"`
	Action Action `json:"action" ub:"action"`
}

func Compose(changes []Change) ([]Change, error) {
	states := make(map[string]state, len(changes))
	for i, change := range changes {
		if !validAction(change.Action) {
			return nil, fmt.Errorf(
				"file change %d for %q has invalid action %q",
				i, change.Path, change.Action,
			)
		}
		state, seen := states[change.Path]
		if !seen {
			state = initialState(change.Action)
			states[change.Path] = state
			continue
		}
		next, err := advanceState(state, change.Action)
		if err != nil {
			return nil, fmt.Errorf("file change %d for %q: %w", i, change.Path, err)
		}
		states[change.Path] = next
	}

	out := make([]Change, 0, len(states))
	for path, state := range states {
		action, ok := composedAction(state)
		if !ok {
			continue
		}
		out = append(out, Change{Path: path, Action: action})
	}
	return Sort(out), nil
}

func Sort(changes []Change) []Change {
	out := append([]Change{}, changes...)
	slices.SortFunc(out, func(a, b Change) int {
		if n := cmp.Compare(a.Path, b.Path); n != 0 {
			return n
		}
		return cmp.Compare(a.Action, b.Action)
	})
	return out
}

type state struct {
	startedPresent bool
	present        bool
	changed        bool
}

func validAction(action Action) bool {
	switch action {
	case ActionCreated, ActionUpdated, ActionRemoved, ActionUnchanged:
		return true
	}
	return false
}

func initialState(action Action) state {
	switch action {
	case ActionCreated:
		return state{present: true, changed: true}
	case ActionUpdated:
		return state{startedPresent: true, present: true, changed: true}
	case ActionRemoved:
		return state{startedPresent: true, changed: true}
	case ActionUnchanged:
		return state{startedPresent: true, present: true}
	}
	panic("invalid file action")
}

func advanceState(current state, action Action) (state, error) {
	if current.present {
		switch action {
		case ActionUpdated:
			current.changed = true
			return current, nil
		case ActionUnchanged:
			return current, nil
		case ActionRemoved:
			current.present = false
			current.changed = true
			return current, nil
		case ActionCreated:
			return state{}, fmt.Errorf("action %q requires an absent path", action)
		}
	}
	if action != ActionCreated {
		return state{}, fmt.Errorf("action %q requires a present path", action)
	}
	current.present = true
	current.changed = true
	return current, nil
}

func composedAction(current state) (Action, bool) {
	switch {
	case !current.startedPresent && !current.present:
		return "", false
	case !current.startedPresent:
		return ActionCreated, true
	case !current.present:
		return ActionRemoved, true
	case current.changed:
		return ActionUpdated, true
	default:
		return ActionUnchanged, true
	}
}
