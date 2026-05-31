package itemcode

import (
	"strings"
	"unicode"
)

// Analysis describes invisible or combining characters inside an SML item code.
type Analysis struct {
	HasHiddenChars bool     `json:"has_hidden_chars"`
	CleanItemCode  string   `json:"clean_item_code,omitempty"`
	Kinds          []string `json:"hidden_char_kinds,omitempty"`
}

// Inspect detects characters that are visually easy to miss in item_code,
// such as BOM, zero-width format runes, and Thai combining marks.
func Inspect(code string) Analysis {
	var b strings.Builder
	b.Grow(len(code))

	seen := map[string]bool{}
	kinds := []string{}
	hidden := false
	for _, r := range code {
		kind, ok := hiddenKind(r)
		if ok {
			hidden = true
			if !seen[kind] {
				seen[kind] = true
				kinds = append(kinds, kind)
			}
			continue
		}
		b.WriteRune(r)
	}
	if !hidden {
		return Analysis{}
	}
	return Analysis{
		HasHiddenChars: true,
		CleanItemCode:  strings.TrimSpace(b.String()),
		Kinds:          kinds,
	}
}

func hiddenKind(r rune) (string, bool) {
	switch r {
	case '\uFEFF':
		return "bom", true
	case '\u200B', '\u200C', '\u200D', '\u2060', '\u180E':
		return "zero_width", true
	}
	switch {
	case unicode.Is(unicode.Cf, r):
		return "format", true
	case unicode.Is(unicode.Mn, r), unicode.Is(unicode.Me, r):
		return "combining_mark", true
	default:
		return "", false
	}
}
