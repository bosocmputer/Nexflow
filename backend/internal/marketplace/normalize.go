package marketplace

import (
	"regexp"
	"strings"
	"unicode"
)

var (
	spaceRE       = regexp.MustCompile(`\s+`)
	optionNoRE    = regexp.MustCompile(`(?i)\bno\.?\s*([0-9]+)`)
	optionNameRE  = regexp.MustCompile(`(?i)(ชื่อสี|สี|option|ตัวเลือก)\s*[:：]?\s*`)
	marketingWord = []string{
		"ของแท้", "ขายดี", "พร้อมส่ง", "ราคาประหยัด", "เครื่องสำอาง",
		"official", "แท้", "100%",
	}
	colorWords = []string{
		"เขียว", "ม่วง", "ชมพู", "ฟ้า", "น้ำเงิน", "น้ำตาลเข้ม",
		"น้ำตาลอ่อน", "น้ำตาล", "ดำ", "ขาว", "แดง", "ส้ม", "เทา",
	}
)

// NormalizeKey turns noisy marketplace product titles into a stable alias key.
// It intentionally keeps option/color tokens because those usually identify the
// actual SML variant for cosmetics and marketplace bundles.
func NormalizeKey(rawName, sourceSKU string) string {
	s := strings.ToLower(strings.TrimSpace(rawName))
	s = strings.ReplaceAll(s, "\ufeff", "")
	for _, w := range marketingWord {
		s = strings.ReplaceAll(s, strings.ToLower(w), " ")
	}
	s = optionNameRE.ReplaceAllString(s, " ")
	s = strings.Map(func(r rune) rune {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r), unicode.IsSpace(r):
			return r
		case r == '/' || r == '-' || r == '.':
			return ' '
		default:
			return ' '
		}
	}, s)
	s = optionNoRE.ReplaceAllString(s, " no$1 ")
	s = spaceRE.ReplaceAllString(strings.TrimSpace(s), " ")
	if s == "" {
		return strings.TrimSpace(sourceSKU)
	}
	return s
}

func ExtractVariantTokens(s string) map[string]bool {
	s = strings.ToLower(s)
	tokens := map[string]bool{}
	for _, m := range optionNoRE.FindAllStringSubmatch(s, -1) {
		if len(m) > 1 {
			tokens["no"+m[1]] = true
		}
	}
	for _, color := range colorWords {
		if strings.Contains(s, color) {
			tokens[color] = true
		}
	}
	return tokens
}

// VariantConflict returns true when both strings carry variant tokens but they
// do not overlap. That blocks auto-confirming close-but-wrong color matches.
func VariantConflict(rawName, candidateName string) bool {
	rawTokens := ExtractVariantTokens(rawName)
	candidateTokens := ExtractVariantTokens(candidateName)
	if len(rawTokens) == 0 || len(candidateTokens) == 0 {
		return false
	}
	for t := range rawTokens {
		if candidateTokens[t] {
			return false
		}
	}
	return true
}
