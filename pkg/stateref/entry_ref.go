package stateref

import (
	"fmt"
	"strings"

	"github.com/cloudboss/unobin/pkg/sdk/state"
)

type EntryRef struct {
	Selector state.Selector
	Address  string
}

func Parse(s string) (EntryRef, error) {
	selectorText, address, ok := strings.Cut(s, "@")
	if !ok {
		return EntryRef{}, fmt.Errorf("expected <selector>@<address>, got %s", s)
	}
	if selectorText == "" {
		return EntryRef{}, fmt.Errorf("missing selector in %s", s)
	}
	if address == "" {
		return EntryRef{}, fmt.Errorf("missing address in %s", s)
	}
	selector, err := parseSelector(selectorText)
	if err != nil {
		return EntryRef{}, err
	}
	if err := ValidateAddress(address); err != nil {
		return EntryRef{}, err
	}
	return EntryRef{Selector: selector, Address: address}, nil
}

func parseSelector(s string) (state.Selector, error) {
	parts := strings.Split(s, ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return state.Selector{}, fmt.Errorf("selector must have two segments: alias.export")
	}
	return state.Selector{Alias: parts[0], Export: parts[1]}, nil
}

func ValidateAddress(address string) error {
	rootEnd := strings.IndexAny(address, "./[")
	if rootEnd < 0 || rootEnd == len(address)-1 || address[rootEnd] != '.' {
		return fmt.Errorf("address must start with resource., data., or action")
	}
	root := address[:rootEnd]
	if root != "resource" && root != "data" && root != "action" {
		return fmt.Errorf("address root must be resource, data, or action")
	}
	if err := validateInstanceKeys(address); err != nil {
		return err
	}
	return nil
}

func validateInstanceKeys(address string) error {
	for i := 0; i < len(address); i++ {
		switch address[i] {
		case '[':
			if i+1 >= len(address) || address[i+1] != '\'' {
				return fmt.Errorf("malformed instance key in %s", address)
			}
			end := strings.Index(address[i+2:], "']")
			if end < 0 {
				return fmt.Errorf("malformed instance key in %s", address)
			}
			next := i + 2 + end + 2
			if next < len(address) && address[next] != '/' {
				return fmt.Errorf("malformed instance key in %s", address)
			}
			i = next - 1
		case ']':
			return fmt.Errorf("malformed instance key in %s", address)
		}
	}
	return nil
}

func (r EntryRef) String() string {
	return r.Selector.Alias + "." + r.Selector.Export + "@" + r.Address
}

func Same(a, b EntryRef) bool {
	return a.Selector.Alias == b.Selector.Alias &&
		a.Selector.Export == b.Selector.Export &&
		a.Address == b.Address
}
