package compiler

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDiscoverSources(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "api/users/[id]/user.go", "package main\nfunc main() {}\n")
	writeFile(t, root, "api/users/user_test.go", "package main\n")
	writeFile(t, root, "middleware/middleware.go", "package main\nfunc main() {}\n")
	writeFile(t, root, "controllers/user.go", "package controllers\n")

	sources, err := DiscoverSources(Config{Root: root})
	if err != nil {
		t.Fatalf("DiscoverSources returned error: %v", err)
	}
	if len(sources) != 2 {
		t.Fatalf("expected 2 plugin sources, got %d: %#v", len(sources), sources)
	}

	expected := map[string]bool{
		filepath.Join(root, "api", "users", "[id]", "user.go"): false,
		filepath.Join(root, "middleware", "middleware.go"):     false,
	}
	for _, source := range sources {
		if _, ok := expected[source.Path]; !ok {
			t.Fatalf("unexpected source: %s", source.Path)
		}
		expected[source.Path] = true
	}
	for source, found := range expected {
		if !found {
			t.Fatalf("expected source not found: %s", source)
		}
	}
}

func TestDefaultRestartDelayIsImmediate(t *testing.T) {
	cfg, err := (Config{}).normalize()
	if err != nil {
		t.Fatalf("normalize returned error: %v", err)
	}
	if cfg.RestartDelay != 0 {
		t.Fatalf("expected default restart delay to be 0, got %s", cfg.RestartDelay)
	}
}

func TestNoInitialBuildConfig(t *testing.T) {
	cfg, err := (Config{NoInitialBuild: true}).normalize()
	if err != nil {
		t.Fatalf("normalize returned error: %v", err)
	}
	if !cfg.NoInitialBuild {
		t.Fatal("expected NoInitialBuild to stay enabled")
	}
}

func TestMaxWorkersConfig(t *testing.T) {
	cfg, err := (Config{MaxWorkers: 4}).normalize()
	if err != nil {
		t.Fatalf("normalize returned error: %v", err)
	}
	if cfg.MaxWorkers != 4 {
		t.Fatalf("expected MaxWorkers to stay configured, got %d", cfg.MaxWorkers)
	}
}

func TestPluginPath(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "api", "users", "[id]", "user.go")
	writeFile(t, root, "api/users/[id]/user.go", "package main\nfunc main() {}\n")

	got, err := PluginPath(Config{Root: root}, source)
	if err != nil {
		t.Fatalf("PluginPath returned error: %v", err)
	}

	want := filepath.Join(root, ".yogourt", "api", "users", "[id]", "user.go.so")
	if got != want {
		t.Fatalf("expected %s, got %s", want, got)
	}
}

func TestChanged(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "api/users/user.go", "package main\nfunc main() {}\n")

	cfg := Config{Root: root}
	snapshot, err := Snapshot(cfg)
	if err != nil {
		t.Fatalf("Snapshot returned error: %v", err)
	}

	time.Sleep(10 * time.Millisecond)
	writeFile(t, root, "api/users/user.go", "package main\nfunc main() {}\nfunc GET() {}\n")

	_, changed, err := Changed(cfg, snapshot)
	if err != nil {
		t.Fatalf("Changed returned error: %v", err)
	}
	if !changed {
		t.Fatal("expected change to be detected")
	}
}

func TestChangedFiles(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "api/users/user.go", "package main\nfunc main() {}\n")

	cfg := Config{Root: root}
	snapshot, err := Snapshot(cfg)
	if err != nil {
		t.Fatalf("Snapshot returned error: %v", err)
	}

	time.Sleep(10 * time.Millisecond)
	writeFile(t, root, "api/users/user.go", "package main\nfunc main() {}\nfunc GET() {}\n")

	_, changedFiles, err := ChangedFiles(cfg, snapshot)
	if err != nil {
		t.Fatalf("ChangedFiles returned error: %v", err)
	}
	if len(changedFiles) != 1 {
		t.Fatalf("expected 1 changed file, got %d: %#v", len(changedFiles), changedFiles)
	}

	want := filepath.Join(root, "api", "users", "user.go")
	if changedFiles[0] != want {
		t.Fatalf("expected changed file %s, got %s", want, changedFiles[0])
	}
}

func TestSourcesForChangedRouteFile(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "api/users/user.go", "package main\nfunc main() {}\n")
	writeFile(t, root, "middleware/middleware.go", "package main\nfunc main() {}\n")

	changedFile := filepath.Join(root, "api", "users", "user.go")
	sources, fullRebuild, err := SourcesForChangedFiles(Config{Root: root}, []string{changedFile})
	if err != nil {
		t.Fatalf("SourcesForChangedFiles returned error: %v", err)
	}
	if fullRebuild {
		t.Fatal("expected partial rebuild for route file")
	}
	if len(sources) != 1 || sources[0].Path != changedFile {
		t.Fatalf("expected only %s, got %#v", changedFile, sources)
	}
}

func TestSourcesForChangedMiddlewareFile(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "api/users/user.go", "package main\nfunc main() {}\n")
	writeFile(t, root, "middleware/middleware.go", "package main\nfunc main() {}\n")

	changedFile := filepath.Join(root, "middleware", "middleware.go")
	sources, fullRebuild, err := SourcesForChangedFiles(Config{Root: root}, []string{changedFile})
	if err != nil {
		t.Fatalf("SourcesForChangedFiles returned error: %v", err)
	}
	if fullRebuild {
		t.Fatal("expected partial rebuild for middleware file")
	}
	if len(sources) != 1 || sources[0].Path != changedFile {
		t.Fatalf("expected only %s, got %#v", changedFile, sources)
	}
}

func TestSourcesForChangedSharedFileForcesFullRebuild(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "api/users/user.go", "package main\nfunc main() {}\n")
	writeFile(t, root, "controllers/user.go", "package controllers\n")

	changedFile := filepath.Join(root, "controllers", "user.go")
	_, fullRebuild, err := SourcesForChangedFiles(Config{Root: root}, []string{changedFile})
	if err != nil {
		t.Fatalf("SourcesForChangedFiles returned error: %v", err)
	}
	if !fullRebuild {
		t.Fatal("expected full rebuild for shared file")
	}
}

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()

	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
}
