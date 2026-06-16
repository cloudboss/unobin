package runner

// sensitivePlaceholder is the literal renderers print in place of a
// masked value.
const sensitivePlaceholder = "***"

func rootSensitiveOutputs(parsed *parsedFactory) map[string]bool {
	if parsed == nil {
		return map[string]bool{}
	}
	return parsed.sensitiveOutputs()
}
