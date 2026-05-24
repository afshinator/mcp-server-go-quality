package version

import (
	"fmt"
	"runtime/debug"
)

var Value = "v0.1.0"

var tagged = ""

func String() string {
	if tagged == "true" {
		return Value
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return Value
	}
	return formatVersion(Value, info.Settings)
}

func formatVersion(base string, settings []debug.BuildSetting) string {
	var commit string
	var dirty bool
	for _, s := range settings {
		switch s.Key {
		case "vcs.revision":
			n := min(7, len(s.Value))
			commit = s.Value[:n]
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if commit == "" {
		return base
	}
	suffix := commit
	if dirty {
		suffix += "-dirty"
	}
	return fmt.Sprintf("%s (%s)", base, suffix)
}
