package tools

import "testing"

func TestBuildTimelineOpenCommands(t *testing.T) {
	tests := []struct {
		name    string
		goos    string
		target  string
		wantLen int
		first   openCommandSpec
	}{
		{
			name:    "windows prefers edge app mode then shell fallback",
			goos:    "windows",
			target:  "file:///tmp/project_timeline.html",
			wantLen: 2,
			first: openCommandSpec{
				name: "cmd",
				args: []string{"/c", "start", "", "msedge", "--app=file:///tmp/project_timeline.html"},
			},
		},
		{
			name:    "darwin uses open",
			goos:    "darwin",
			target:  "file:///tmp/project_timeline.html",
			wantLen: 1,
			first: openCommandSpec{
				name: "open",
				args: []string{"file:///tmp/project_timeline.html"},
			},
		},
		{
			name:    "linux uses xdg-open first",
			goos:    "linux",
			target:  "file:///tmp/project_timeline.html",
			wantLen: 2,
			first: openCommandSpec{
				name: "xdg-open",
				args: []string{"file:///tmp/project_timeline.html"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildTimelineOpenCommands(tt.goos, tt.target)
			if len(got) != tt.wantLen {
				t.Fatalf("len(commands) = %d, want %d", len(got), tt.wantLen)
			}
			if got[0].name != tt.first.name {
				t.Fatalf("first command name = %q, want %q", got[0].name, tt.first.name)
			}
			if len(got[0].args) != len(tt.first.args) {
				t.Fatalf("first command args len = %d, want %d", len(got[0].args), len(tt.first.args))
			}
			for i := range tt.first.args {
				if got[0].args[i] != tt.first.args[i] {
					t.Fatalf("first command args[%d] = %q, want %q", i, got[0].args[i], tt.first.args[i])
				}
			}
		})
	}
}
