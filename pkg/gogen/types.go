package gogen

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

// pascalToKebab converts a PascalCase string to kebab-case.
func pascalToKebab(s string) string {
	if s == "" {
		return s
	}
	var b []byte
	for i, c := range s {
		if c >= 'A' && c <= 'Z' {
			if i > 0 {
				prev := s[i-1]
				if prev >= 'a' && prev <= 'z' {
					b = append(b, '-')
				} else if prev >= '0' && prev <= '9' {
					b = append(b, '-')
				} else if i+1 < len(s) && s[i+1] >= 'a' && s[i+1] <= 'z' &&
					prev >= 'A' && prev <= 'Z' {
					b = append(b, '-')
				}
			}
			b = append(b, byte(c)+32)
		} else {
			b = append(b, byte(c))
		}
	}
	return string(b)
}

// MapstructureTag returns the mapstructure tag value for a field name.
func MapstructureTag(fieldName string) string {
	return pascalToKebab(fieldName)
}
