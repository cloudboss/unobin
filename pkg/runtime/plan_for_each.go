package runtime

import (
	"strings"
)

// rewriteAddress substitutes the for-each boundary's template address
// with its per-instance form everywhere it appears as a prefix in
// addr. The boundary itself reduces to instAddr; an internal at
// `<boundary>/<inner>` becomes `<instAddr>/<inner>`.
func rewriteAddress(addr, boundary, instAddr string) string {
	if addr == boundary {
		return instAddr
	}
	prefix := boundary + "/"
	if strings.HasPrefix(addr, prefix) {
		return instAddr + "/" + addr[len(prefix):]
	}
	return addr
}
