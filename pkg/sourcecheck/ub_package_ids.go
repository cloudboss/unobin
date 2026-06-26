package sourcecheck

import (
	"crypto/sha256"
	"fmt"
	"go/token"
	"strings"
)

type ubPackageIDs struct {
	byKey map[string]string
	used  map[string]string
}

func newUBPackageIDs() *ubPackageIDs {
	return &ubPackageIDs{
		byKey: map[string]string{},
		used:  map[string]string{},
	}
}

func (ids *ubPackageIDs) ID(alias string, canonicalKey string) string {
	if id, ok := ids.byKey[canonicalKey]; ok {
		return id
	}

	base := sanitizeGoPackageID(alias)
	if ids.packageIDAvailable(base, canonicalKey) {
		ids.assign(canonicalKey, base)
		return base
	}

	sum := sha256.Sum256([]byte(canonicalKey))
	for bytesUsed := 4; bytesUsed <= len(sum); bytesUsed++ {
		id := fmt.Sprintf("%s_%x", base, sum[:bytesUsed])
		if ids.packageIDAvailable(id, canonicalKey) {
			ids.assign(canonicalKey, id)
			return id
		}
	}

	full := fmt.Sprintf("%s_%x", base, sum)
	for i := 2; ; i++ {
		id := fmt.Sprintf("%s_%d", full, i)
		if ids.packageIDAvailable(id, canonicalKey) {
			ids.assign(canonicalKey, id)
			return id
		}
	}
}

func (ids *ubPackageIDs) packageIDAvailable(id string, canonicalKey string) bool {
	owner, used := ids.used[id]
	return !used || owner == canonicalKey
}

func (ids *ubPackageIDs) assign(canonicalKey string, id string) {
	ids.byKey[canonicalKey] = id
	ids.used[id] = canonicalKey
}

func sanitizeGoPackageID(s string) string {
	var b strings.Builder
	for i, r := range s {
		switch {
		case r == '-' || r == '.':
			b.WriteRune('_')
		case r >= '0' && r <= '9':
			if i == 0 {
				b.WriteRune('_')
			}
			b.WriteRune(r)
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	if b.Len() == 0 {
		return "x"
	}
	out := b.String()
	if token.Lookup(out).IsKeyword() {
		return "_" + out
	}
	return out
}
