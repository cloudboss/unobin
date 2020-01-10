package util

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
