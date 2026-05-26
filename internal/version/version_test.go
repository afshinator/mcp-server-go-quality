package version

import (
	"runtime/debug"
	"testing"
)

func TestStringNotEmpty(t *testing.T) {
	s := String()
	if s == "" {
		t.Error("version string must not be empty")
	}
}

func TestValueDefault(t *testing.T) {
	if Value == "" {
		t.Error("Value must have a default")
	}
}

func TestFormatVersion(t *testing.T) {
	tests := []struct {
		name     string
		base     string
		settings []debug.BuildSetting
		want     string
	}{
		{
			name: "clean commit",
			base: "v0.1.0",
			settings: []debug.BuildSetting{
				{Key: "vcs.revision", Value: "abcdef1234567890"},
				{Key: "vcs.modified", Value: "false"},
			},
			want: "v0.1.0 (abcdef1)",
		},
		{
			name: "dirty workspace",
			base: "v0.1.0",
			settings: []debug.BuildSetting{
				{Key: "vcs.revision", Value: "abcdef1234567890"},
				{Key: "vcs.modified", Value: "true"},
			},
			want: "v0.1.0 (abcdef1-dirty)",
		},
		{
			name: "no VCS metadata",
			base: "v0.1.0",
			want: "v0.1.0",
		},
		{
			name: "short commit hash",
			base: "v0.1.0",
			settings: []debug.BuildSetting{
				{Key: "vcs.revision", Value: "abc"},
				{Key: "vcs.modified", Value: "false"},
			},
			want: "v0.1.0 (abc)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatVersion(tt.base, tt.settings)
			if got != tt.want {
				t.Errorf("formatVersion() = %q, want %q", got, tt.want)
			}
		})
	}
}
