package lang

import "strings"

// PascalToKebab converts a PascalCase Go identifier to its kebab-case
// UB form. A '-' goes before a capital that follows a lowercase
// letter or digit, and before the last capital in a stretch of
// capitals that precedes a lowercase letter (so HTTPSProxy becomes
// https-proxy). Non-letter, non-digit bytes pass through unchanged.
func PascalToKebab(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 4)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if i > 0 && c >= 'A' && c <= 'Z' {
			prev := s[i-1]
			next := byte(0)
			if i+1 < len(s) {
				next = s[i+1]
			}
			lowerPrev := prev >= 'a' && prev <= 'z'
			digitPrev := prev >= '0' && prev <= '9'
			lowerNext := next >= 'a' && next <= 'z'
			if lowerPrev || digitPrev || (next != 0 && lowerNext) {
				b.WriteByte('-')
			}
		}
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b.WriteByte(c)
	}
	return b.String()
}
