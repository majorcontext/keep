package engine

import "testing"

func TestGlob_Exact(t *testing.T) {
	tests := []struct {
		pattern string
		name    string
		want    bool
	}{
		{"create_issue", "create_issue", true},
		{"create_issue", "delete_issue", false},
	}
	for _, tt := range tests {
		t.Run(tt.pattern+"/"+tt.name, func(t *testing.T) {
			got := GlobMatch(tt.pattern, tt.name)
			if got != tt.want {
				t.Errorf("GlobMatch(%q, %q) = %v, want %v", tt.pattern, tt.name, got, tt.want)
			}
		})
	}
}

func TestGlob_Star(t *testing.T) {
	tests := []struct {
		pattern string
		name    string
		want    bool
	}{
		{"create_*", "create_issue", true},
		{"create_*", "create_comment", true},
		{"create_*", "delete_issue", false},
	}
	for _, tt := range tests {
		t.Run(tt.pattern+"/"+tt.name, func(t *testing.T) {
			got := GlobMatch(tt.pattern, tt.name)
			if got != tt.want {
				t.Errorf("GlobMatch(%q, %q) = %v, want %v", tt.pattern, tt.name, got, tt.want)
			}
		})
	}
}

func TestGlob_Question(t *testing.T) {
	tests := []struct {
		pattern string
		name    string
		want    bool
	}{
		{"llm.tool_?", "llm.tool_a", true},
		{"llm.tool_?", "llm.tool_use", false},
	}
	for _, tt := range tests {
		t.Run(tt.pattern+"/"+tt.name, func(t *testing.T) {
			got := GlobMatch(tt.pattern, tt.name)
			if got != tt.want {
				t.Errorf("GlobMatch(%q, %q) = %v, want %v", tt.pattern, tt.name, got, tt.want)
			}
		})
	}
}

func TestGlob_StarAll(t *testing.T) {
	tests := []struct {
		pattern string
		name    string
		want    bool
	}{
		{"*", "create_issue", true},
		{"*", "delete_comment", true},
		{"*", "llm.tool_use", true},
	}
	for _, tt := range tests {
		t.Run(tt.pattern+"/"+tt.name, func(t *testing.T) {
			got := GlobMatch(tt.pattern, tt.name)
			if got != tt.want {
				t.Errorf("GlobMatch(%q, %q) = %v, want %v", tt.pattern, tt.name, got, tt.want)
			}
		})
	}
}

func TestGlob_DotStar(t *testing.T) {
	tests := []struct {
		pattern string
		name    string
		want    bool
	}{
		{"llm.*", "llm.request", true},
		{"llm.*", "llm.tool_use", true},
		{"llm.*", "create_issue", false},
	}
	for _, tt := range tests {
		t.Run(tt.pattern+"/"+tt.name, func(t *testing.T) {
			got := GlobMatch(tt.pattern, tt.name)
			if got != tt.want {
				t.Errorf("GlobMatch(%q, %q) = %v, want %v", tt.pattern, tt.name, got, tt.want)
			}
		})
	}
}

func TestGlob_Empty(t *testing.T) {
	tests := []struct {
		pattern string
		name    string
		want    bool
	}{
		{"", "create_issue", true},
		{"", "llm.tool_use", true},
		{"", "anything", true},
	}
	for _, tt := range tests {
		t.Run(tt.pattern+"/"+tt.name, func(t *testing.T) {
			got := GlobMatch(tt.pattern, tt.name)
			if got != tt.want {
				t.Errorf("GlobMatch(%q, %q) = %v, want %v", tt.pattern, tt.name, got, tt.want)
			}
		})
	}
}
