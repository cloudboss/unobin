package util

import (
	"strings"

	"github.com/cloudboss/unobin/pkg/types"
)

func FilterEmpty(items []string) []string {
	nonEmpty := []string{}
	for _, item := range items {
		if item != "" {
			nonEmpty = append(nonEmpty, item)
		}
	}
	return nonEmpty
}

func Any(bools []bool) bool {
	for _, b := range bools {
		if b {
			return b
		}
	}
	return false
}

func All(bools []bool) bool {
	for _, b := range bools {
		if !b {
			return false
		}
	}
	return true
}

func ErrResult(msg, moduleName string) *types.Result {
	return &types.Result{
		Succeeded: false,
		Changed:   false,
		Error:     msg,
		Module:    moduleName,
	}
}

// snakeToThing converts a "snake_case" string to either a "PascalCase" or a "camelCase" string.
// The firstUpper argument determines if the first letter of the returned string is lower case.
func snakeToThing(s string, firstLower bool) string {
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == '_' })
	capitalizedParts := []string{}
	for i, part := range parts {
		var first string
		if i == 0 && firstLower {
			first = strings.ToLower(string(part[0]))
		} else {
			first = strings.ToUpper(string(part[0]))
		}
		rest := ""
		if len(part) >= 2 {
			rest = string(part[1:])
		}
		capitalizedParts = append(capitalizedParts, first+rest)
	}
	return strings.Join(capitalizedParts, "")
}

// SnakeToPascal converts a "snake_case" string to a "PascalCase" string.
func SnakeToPascal(s string) string {
	return snakeToThing(s, false)
}

// SnakeToCamel converts a "snake_case" string to a "camelCase" string.
func SnakeToCamel(s string) string {
	return snakeToThing(s, true)
}
