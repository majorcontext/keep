package cel

import "testing"

func TestRewriteHasSecrets(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "single arg simple",
			in:   "hasSecrets(params.text)",
			want: "hasSecrets(params.text, _originalParams)",
		},
		{
			name: "single arg nested field",
			in:   "hasSecrets(params.input.command)",
			want: "hasSecrets(params.input.command, _originalParams)",
		},
		{
			name: "already two args",
			in:   "hasSecrets(params.text, _originalParams)",
			want: "hasSecrets(params.text, _originalParams)",
		},
		{
			name: "no hasSecrets",
			in:   "params.name == 'bash'",
			want: "params.name == 'bash'",
		},
		{
			name: "embedded in expression",
			in:   "params.name == 'bash' && hasSecrets(params.text)",
			want: "params.name == 'bash' && hasSecrets(params.text, _originalParams)",
		},
		{
			name: "multiple calls",
			in:   "hasSecrets(params.a) || hasSecrets(params.b)",
			want: "hasSecrets(params.a, _originalParams) || hasSecrets(params.b, _originalParams)",
		},
		{
			name: "nested function call as arg",
			in:   "hasSecrets(lower(params.text))",
			want: "hasSecrets(lower(params.text), _originalParams)",
		},
		{
			name: "string with parens",
			in:   `hasSecrets(params.text) && params.x == "foo(bar)"`,
			want: `hasSecrets(params.text, _originalParams) && params.x == "foo(bar)"`,
		},
		{
			name: "empty expression",
			in:   "",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RewriteHasSecrets(tt.in)
			if got != tt.want {
				t.Errorf("RewriteHasSecrets(%q)\n  got  %q\n  want %q", tt.in, got, tt.want)
			}
		})
	}
}
