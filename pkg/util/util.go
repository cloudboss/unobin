package util

import (
	"strings"

	"github.com/cloudboss/go-player/pkg/types"
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

// SnakeToPascal converts a "snake_case" string to a "PascalCase" string.
func SnakeToPascal(s string) string {
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == '_' })
	capitalized_parts := []string{}
	for _, part := range parts {
		first := strings.ToUpper(string(part[0]))
		rest := ""
		if len(part) >= 2 {
			rest = string(part[1:])
		}
		capitalized_parts = append(capitalized_parts, first+rest)
	}
	return strings.Join(capitalized_parts, "")
}
