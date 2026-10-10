package nip54

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// NormalizeIdentifier applies the NIP-54 d tag rules: lowercase, whitespace to "-",
// punctuation and symbols removed, runs of "-" collapsed, leading and trailing "-" removed,
// and letters and numbers of any script preserved. Combining marks stay with their letters,
// and "-" itself is a separator, so a normalized identifier normalizes to itself.
func NormalizeIdentifier(name string) string {
	name = norm.NFKC.String(strings.ToLower(name))

	var b strings.Builder
	dash := false
	for _, r := range name {
		switch {
		case unicode.IsLetter(r) || unicode.IsNumber(r) || unicode.IsMark(r):
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			dash = false
			b.WriteRune(unicode.ToLower(r))
		case unicode.IsSpace(r) || r == '-':
			dash = true
		}
	}

	return b.String()
}
