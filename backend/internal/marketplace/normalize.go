package marketplace

import (
	"regexp"
	"strings"
)

var spaceRE = regexp.MustCompile(`\s+`)

// NormalizeKey removes only BOM and insignificant whitespace. It deliberately
// preserves case, punctuation, and every product-name token.
func NormalizeKey(rawName, sourceSKU string) string {
	s := strings.ReplaceAll(rawName, "\ufeff", "")
	s = spaceRE.ReplaceAllString(strings.TrimSpace(s), " ")
	if s != "" {
		return s
	}
	s = strings.ReplaceAll(sourceSKU, "\ufeff", "")
	return spaceRE.ReplaceAllString(strings.TrimSpace(s), " ")
}
