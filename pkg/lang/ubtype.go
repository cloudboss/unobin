package lang

import "fmt"

// TypeMessage names a parsed value with the unobin type vocabulary
// so error messages match what the operator wrote, not the Go runtime
// type they would never see otherwise.
func TypeMessage(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case string:
		return "a string"
	case bool:
		return "a boolean"
	case int, int32, int64:
		return "an integer"
	case float32, float64:
		return "a number"
	case []any:
		return "a list"
	case map[string]any:
		return "an object"
	default:
		return fmt.Sprintf("%T", v)
	}
}
