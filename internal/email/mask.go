package email

import (
	"strings"
	"unicode/utf8"
)

// MaskAddress hides an email's local part for logging.
func MaskAddress(addr string) string {
	at := strings.LastIndex(addr, "@")
	if at < 1 || at == len(addr)-1 {
		return "***"
	}
	_, size := utf8.DecodeRuneInString(addr)
	return addr[:size] + "***" + addr[at:]
}
