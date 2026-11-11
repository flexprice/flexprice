package email

import (
	"strings"
	"unicode/utf8"
)

// MaskAddress hides the local part.
func MaskAddress(addr string) string {
	at := strings.LastIndex(addr, "@")
	if at < 1 || at == len(addr)-1 {
		return "***"
	}
	_, size := utf8.DecodeRuneInString(addr)
	return addr[:size] + "***" + addr[at:]
}
