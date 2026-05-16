package gogen

import "github.com/cloudboss/unobin/pkg/lang"

// PointerType returns the pointer-wrapped Go type for optional fields.
// For bare types (string, int64, bool, float64) it returns "*T".
// For reference types ([]T, map[K]V, any) it returns the type unchanged
// since slices, maps, and interfaces are nil-able by default.
func PointerType(goType string) string {
	switch goType {
	case "string", "int64", "bool", "float64":
		return "*" + goType
	default:
		return goType
	}
}

// MapstructureTag returns the mapstructure tag value for a field name.
func MapstructureTag(fieldName string) string {
	return lang.PascalToKebab(fieldName)
}
