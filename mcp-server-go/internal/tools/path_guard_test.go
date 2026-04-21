package tools

import "testing"

func TestNormalizeProjectRelativePath(t *testing.T) {
	projectRoot := "D:/AI_Project/MPM-Coding"

	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		{name: "empty stays empty", raw: "", want: ""},
		{name: "normalize separators", raw: ".\\internal\\services\\..\\services", want: "internal/services"},
		{name: "reject absolute", raw: "D:/other/repo", wantErr: true},
		{name: "reject traversal", raw: "../outside", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeProjectRelativePath(projectRoot, tt.raw, "scope")
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got none (value=%q)", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("normalizeProjectRelativePath(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}
