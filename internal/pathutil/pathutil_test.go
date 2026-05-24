package pathutil

import "testing"

func TestRel(t *testing.T) {
	projectRoot := "/project/myapp"

	tests := []struct {
		name    string
		absPath string
		want    string
	}{
		{
			name:    "simple relative",
			absPath: "/project/myapp/cmd/main.go",
			want:    "cmd/main.go",
		},
		{
			name:    "deep relative",
			absPath: "/project/myapp/internal/auth/auth.go",
			want:    "internal/auth/auth.go",
		},
		{
			name:    "already relative path",
			absPath: "cmd/main.go",
			want:    "cmd/main.go",
		},
		{
			name:    "different root (falls back to trimprefix)",
			absPath: "/other/project/file.go",
			want:    "/other/project/file.go",
		},
		{
			name:    "empty path",
			absPath: "",
			want:    "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Rel(projectRoot, tt.absPath)
			if got != tt.want {
				t.Errorf("Rel(%q, %q) = %q, want %q", projectRoot, tt.absPath, got, tt.want)
			}
		})
	}
}
