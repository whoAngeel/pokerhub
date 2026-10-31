package llm

import "testing"

func TestExtractBalancedJSON(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{
			name: "plain object",
			in:   `{"action":"check"}`,
			want: `{"action":"check"}`,
		},
		{
			name: "prose before and after",
			in:   `Sure, here is my decision: {"action":"fold"} hope that works!`,
			want: `{"action":"fold"}`,
		},
		{
			name: "nested braces",
			in:   `{"action":"raise","reasoning":{"hand":"AA","plan":"go big"}}`,
			want: `{"action":"raise","reasoning":{"hand":"AA","plan":"go big"}}`,
		},
		{
			name: "brace inside string literal",
			in:   `{"say":"gg { wp }","action":"check"}`,
			want: `{"say":"gg { wp }","action":"check"}`,
		},
		{
			name: "escaped quote inside string",
			in:   `{"say":"he said \"hi\" {not a brace}","action":"check"}`,
			want: `{"say":"he said \"hi\" {not a brace}","action":"check"}`,
		},
		{
			name: "fenced with json tag",
			in:   "```json\n{\"action\":\"call\"}\n```",
			want: `{"action":"call"}`,
		},
		{
			name: "fenced without language tag",
			in:   "```\n{\"action\":\"call\"}\n```",
			want: `{"action":"call"}`,
		},
		{
			name: "fenced with surrounding prose",
			in:   "Here you go:\n```json\n{\"action\":\"call\"}\n```\nenjoy",
			want: `{"action":"call"}`,
		},
		{
			name:    "no object at all",
			in:      "sorry, I can't help with that",
			wantErr: true,
		},
		{
			name:    "unbalanced braces",
			in:      `{"action":"check"`,
			wantErr: true,
		},
		{
			name:    "empty string",
			in:      "",
			wantErr: true,
		},
		{
			name: "multiple objects picks the first balanced one",
			in:   `{"a":1} {"b":2}`,
			want: `{"a":1}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := extractBalancedJSON(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("extractBalancedJSON(%q) error = nil, want error", tt.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("extractBalancedJSON(%q) unexpected error: %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("extractBalancedJSON(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
