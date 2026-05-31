package sml

import (
	"bytes"
	"encoding/json"
	"fmt"
	"unicode/utf8"
)

// marshalASCII serialises v as JSON but escapes every non-ASCII rune as
// \uXXXX (or surrogate pairs for code points outside the BMP).
//
// Why: SML 248's Java backend reads the request body using ISO-8859-1
// (Latin-1) regardless of the Content-Type charset header, so any UTF-8
// multi-byte sequence we send for Thai gets re-decoded byte-by-byte and
// stored as mojibake. Encoding non-ASCII as \uXXXX keeps the wire body
// pure ASCII — Latin-1 vs UTF-8 are byte-identical for the ASCII range —
// and the standard JSON parser on the server unescapes the \u escapes
// back into the correct Unicode code points before SML's own code sees
// the strings.
//
// Use this in place of json.Marshal for every Nexflow → SML POST body.
func marshalASCII(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return asciiEscapeJSON(raw), nil
}

// MarshalASCII is the exported form for handlers that post directly to
// sml-api-bybos endpoints outside the dedicated SML clients.
func MarshalASCII(v any) ([]byte, error) {
	return marshalASCII(v)
}

// asciiEscapeJSON walks an already-marshalled JSON byte slice and rewrites
// every non-ASCII byte sequence inside string literals as \uXXXX.
// Bytes outside string literals (numbers, structural chars, whitespace) are
// already ASCII in valid JSON and pass through untouched.
func asciiEscapeJSON(in []byte) []byte {
	var buf bytes.Buffer
	buf.Grow(len(in) + len(in)/4)
	inString := false
	escape := false
	i := 0
	for i < len(in) {
		c := in[i]
		if !inString {
			buf.WriteByte(c)
			if c == '"' {
				inString = true
			}
			i++
			continue
		}
		if escape {
			buf.WriteByte(c)
			escape = false
			i++
			continue
		}
		switch c {
		case '\\':
			buf.WriteByte(c)
			escape = true
			i++
			continue
		case '"':
			buf.WriteByte(c)
			inString = false
			i++
			continue
		}
		if c < 0x80 {
			buf.WriteByte(c)
			i++
			continue
		}
		// Multi-byte UTF-8 → decode rune, emit \uXXXX (or surrogate pair).
		r, size := utf8.DecodeRune(in[i:])
		if r == utf8.RuneError && size == 1 {
			// Stray invalid byte — emit literally so we don't corrupt further.
			buf.WriteByte(c)
			i++
			continue
		}
		if r > 0xFFFF {
			r -= 0x10000
			hi := 0xD800 + (r >> 10)
			lo := 0xDC00 + (r & 0x3FF)
			fmt.Fprintf(&buf, "\\u%04x\\u%04x", hi, lo)
		} else {
			fmt.Fprintf(&buf, "\\u%04x", r)
		}
		i += size
	}
	return buf.Bytes()
}
