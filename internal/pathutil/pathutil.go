package pathutil

import (
	"path/filepath"
	"strings"
)

func Rel(projectRoot, inputPath string) string {
	if inputPath == "" {
		return ""
	}

	cleaned := filepath.Clean(inputPath)

	if !filepath.IsAbs(cleaned) {
		return cleaned
	}

	rel, err := filepath.Rel(projectRoot, cleaned)
	if err == nil {
		rel = filepath.Clean(rel)

		if rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return rel
		}
	}

	cleanRoot := filepath.Clean(projectRoot) + string(filepath.Separator)
	if trimmed := strings.TrimPrefix(cleaned, cleanRoot); trimmed != cleaned {
		return trimmed
	}

	return cleaned
}
