package root

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverWithGoWork(t *testing.T) {
	dir := t.TempDir()
	workDir := filepath.Join(dir, "monorepo")
	moduleDir := filepath.Join(workDir, "services", "auth")
	if err := os.MkdirAll(moduleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workDir, "go.work"), []byte("go 1.25\nuse ./services/auth\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(moduleDir, "go.mod"), []byte("module github.com/org/auth\ngo 1.25\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := Discover(moduleDir)
	if err != nil {
		t.Fatal(err)
	}
	if got != workDir {
		t.Errorf("root = %q, want %q (go.work location)", got, workDir)
	}
}

func TestDiscoverWithGoMod(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module github.com/org/app\ngo 1.25\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := Discover(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != dir {
		t.Errorf("root = %q, want %q", got, dir)
	}
}

func TestDiscoverWalkUp(t *testing.T) {
	dir := t.TempDir()
	subDir := filepath.Join(dir, "pkg", "deep", "path")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module github.com/org/app\ngo 1.25\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := Discover(subDir)
	if err != nil {
		t.Fatal(err)
	}
	if got != dir {
		t.Errorf("root = %q, want %q (ancestor with go.mod)", got, dir)
	}
}

func TestDiscoverGoWorkWinsOverCloserGoMod(t *testing.T) {
	dir := t.TempDir()
	workDir := filepath.Join(dir, "monorepo")
	moduleDir := filepath.Join(workDir, "sub")
	if err := os.MkdirAll(moduleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workDir, "go.work"), []byte("go 1.25\nuse ./sub\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(moduleDir, "go.mod"), []byte("module github.com/org/sub\ngo 1.25\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := Discover(moduleDir)
	if err != nil {
		t.Fatal(err)
	}
	if got != workDir {
		t.Errorf("root = %q, want %q (go.work wins over closer go.mod)", got, workDir)
	}
}

func TestDiscoverNotAGoProject(t *testing.T) {
	dir := t.TempDir()
	_, err := Discover(dir)
	if err == nil {
		t.Error("expected error for directory without go.mod or go.work")
	}
}

func TestWorkspaceModulesSingleModule(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module github.com/org/app\ngo 1.25\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	modules, err := WorkspaceModules(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(modules) != 1 || modules[0] != "github.com/org/app" {
		t.Errorf("modules = %v, want [github.com/org/app]", modules)
	}
}

func TestWorkspaceModulesGoWork(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.work"), []byte("go 1.25\nuse ./services/auth\nuse ./lib/common\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	authDir := filepath.Join(dir, "services", "auth")
	commonDir := filepath.Join(dir, "lib", "common")
	if err := os.MkdirAll(authDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(commonDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(authDir, "go.mod"), []byte("module github.com/org/auth\ngo 1.25\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(commonDir, "go.mod"), []byte("module github.com/org/common\ngo 1.25\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	modules, err := WorkspaceModules(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(modules) != 2 {
		t.Fatalf("got %d modules, want 2", len(modules))
	}
}

func TestParseUseDirectivesBlockSyntax(t *testing.T) {
	input := `
go 1.25

use (
    ./service/auth
    ./lib/common
)
`
	got, err := parseUseDirectives(input)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"./service/auth", "./lib/common"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("got %#v want %#v", got, want)
	}
}
