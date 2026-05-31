package sml

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMarshalASCII_AllASCII(t *testing.T) {
	cases := []any{
		map[string]string{"x": "ascii"},
		map[string]string{"name": "สีเพ้น"},
		map[string]string{"name": "ABC สี \"quoted\" \\backslash"},
		map[string]string{"emoji": "🎉"},
		map[string]any{"items": []map[string]string{{"item_name": "ฟอร์ด"}}},
		map[string]any{"qty": 1.5, "thai": "บาท"},
	}
	for i, c := range cases {
		b, err := marshalASCII(c)
		if err != nil {
			t.Fatalf("case %d: marshal err: %v", i, err)
		}
		// Output must be pure ASCII.
		for _, x := range b {
			if x >= 0x80 {
				t.Fatalf("case %d: non-ASCII byte 0x%02x in output: %s", i, x, string(b))
			}
		}
		// Round-trip back to verify the original strings are preserved.
		var orig, decoded any
		_ = json.Unmarshal(mustMarshal(c), &orig)
		if err := json.Unmarshal(b, &decoded); err != nil {
			t.Fatalf("case %d: round-trip parse err: %v — body: %s", i, err, b)
		}
		if !strings.EqualFold(asJSON(orig), asJSON(decoded)) {
			t.Errorf("case %d: round-trip mismatch\norig:    %s\ndecoded: %s", i, asJSON(orig), asJSON(decoded))
		}
	}
}

func mustMarshal(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func asJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func TestMarshalASCII_ThaiProductPayload(t *testing.T) {
	type item struct {
		ItemCode string `json:"item_code"`
		ItemName string `json:"item_name"`
		Qty      int    `json:"qty"`
	}
	payload := struct {
		DocNo string `json:"doc_no"`
		Items []item `json:"items"`
	}{
		DocNo: "NX-SO260400099",
		Items: []item{
			{"HENNA001", "สีเพ้นคิ้วเฮนน่า ฟอร์ด แปรง 15 คู่", 1},
		},
	}
	b, err := marshalASCII(payload)
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	for _, x := range b {
		if x >= 0x80 {
			t.Fatalf("non-ASCII byte 0x%02x in output: %s", x, got)
		}
	}
	// Output should contain literal `สี` — ส (U+0E2A) + ี (U+0E35) escape form.
	want := "\\u0e2a\\u0e35"
	if !strings.Contains(got, want) {
		t.Errorf("expected %q in output, got: %s", want, got)
	}
}
