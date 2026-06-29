package gogen

import (
	"strings"

	"github.com/cloudboss/unobin/pkg/lang"
)

// PointerType returns the pointer-wrapped Go type for optional fields.
// Optional UB fields use Go pointers, including map and slice fields.
func PointerType(goType string) string {
	if goType == "any" || strings.HasPrefix(goType, "*") {
		return goType
	}
	if strings.HasPrefix(goType, "[]") || strings.HasPrefix(goType, "map[") {
		return "*" + goType
	}
	switch goType {
	case "string", "int64", "bool", "float64":
		return "*" + goType
	default:
		return goType
	}
}

// UBTag returns the ub tag value for a field name.
func UBTag(fieldName string) string {
	return lang.PascalToKebab(fieldName)
}
