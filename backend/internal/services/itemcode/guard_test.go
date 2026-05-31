package itemcode

import "testing"

func TestInspectHiddenItemCode(t *testing.T) {
	tests := []struct {
		name  string
		code  string
		want  bool
		clean string
		kinds []string
	}{
		{name: "clean", code: "BILLS002", want: false, clean: ""},
		{name: "thai combining mark", code: "\u0E3ABILLS002", want: true, clean: "BILLS002", kinds: []string{"combining_mark"}},
		{name: "bom", code: "\uFEFFBILLS002", want: true, clean: "BILLS002", kinds: []string{"bom"}},
		{name: "zero width", code: "BILLS\u200B002", want: true, clean: "BILLS002", kinds: []string{"zero_width"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Inspect(tt.code)
			if got.HasHiddenChars != tt.want {
				t.Fatalf("HasHiddenChars = %v, want %v", got.HasHiddenChars, tt.want)
			}
			if got.CleanItemCode != tt.clean {
				t.Fatalf("CleanItemCode = %q, want %q", got.CleanItemCode, tt.clean)
			}
			if len(got.Kinds) != len(tt.kinds) {
				t.Fatalf("Kinds = %#v, want %#v", got.Kinds, tt.kinds)
			}
			for i := range tt.kinds {
				if got.Kinds[i] != tt.kinds[i] {
					t.Fatalf("Kinds = %#v, want %#v", got.Kinds, tt.kinds)
				}
			}
		})
	}
}
