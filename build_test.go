package compiler

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
)

func TestBuildSourcesWarmsDependencyCacheBeforeAnyPlugin(t *testing.T) {
	sources := []Source{{Path: "a.go"}, {Path: "b.go"}, {Path: "c.go"}}
	var warmups atomic.Int32
	var builtBeforeWarmup atomic.Int32

	warm := func(ctx context.Context, cfg Config, warmed []Source) {
		if !reflect.DeepEqual(warmed, sources) {
			t.Errorf("warmed sources = %v, want %v", warmed, sources)
		}
		warmups.Add(1)
	}
	build := func(ctx context.Context, cfg Config, source Source) BuildResult {
		if warmups.Load() == 0 {
			builtBeforeWarmup.Add(1)
		}
		return BuildResult{Source: source.Path}
	}

	report := buildSourcesWith(context.Background(), Config{MaxWorkers: 3}, sources, warm, build)

	if got := warmups.Load(); got != 1 {
		t.Fatalf("cache warmed %d times, want 1", got)
	}
	if got := builtBeforeWarmup.Load(); got != 0 {
		t.Fatalf("%d plugins started before the cache was warmed", got)
	}
	if !report.Success || len(report.Results) != len(sources) {
		t.Fatalf("report = %+v, want success with %d results", report, len(sources))
	}
	for i, result := range report.Results {
		if result.Source != sources[i].Path {
			t.Errorf("results[%d].Source = %q, want %q", i, result.Source, sources[i].Path)
		}
	}
}

func TestBuildSourcesSkipsWarmupForSinglePlugin(t *testing.T) {
	warm := func(ctx context.Context, cfg Config, warmed []Source) {
		t.Error("cache warmed for a single plugin")
	}
	build := func(ctx context.Context, cfg Config, source Source) BuildResult {
		return BuildResult{Source: source.Path}
	}

	report := buildSourcesWith(context.Background(), Config{}, []Source{{Path: "a.go"}}, warm, build)

	if !report.Success || len(report.Results) != 1 {
		t.Fatalf("report = %+v, want success with 1 result", report)
	}
}

func TestSourceImportsMergesImportsOfEveryPlugin(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"a.go":      "package main\n\nimport (\n\t\"fmt\"\n\tgin \"github.com/gin-gonic/gin\"\n)\n",
		"b.go":      "package main\n\nimport \"C\"\nimport (\n\t\"fmt\"\n\t_ \"example.com/app/services\"\n)\n",
		"broken.go": "this is not go",
	}
	sources := make([]Source, 0, len(files)+1)
	for _, name := range []string{"a.go", "b.go", "broken.go"} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(files[name]), 0o644); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, Source{Path: path})
	}
	sources = append(sources, Source{Path: filepath.Join(dir, "missing.go")})

	got := sourceImports(sources)
	want := []string{"example.com/app/services", "fmt", "github.com/gin-gonic/gin"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sourceImports = %v, want %v", got, want)
	}
}

func TestWarmupProgramBlankImportsEveryDependency(t *testing.T) {
	got := warmupProgram([]string{"fmt", "github.com/gin-gonic/gin"})
	want := "package main\n\nimport (\n\t_ \"fmt\"\n\t_ \"github.com/gin-gonic/gin\"\n)\n\nfunc main() {}\n"
	if got != want {
		t.Fatalf("warmupProgram = %q, want %q", got, want)
	}
}

func TestPluginBuildArgsStripDwarfOnDarwin(t *testing.T) {
	source := Source{Path: "api/health/route.go", PluginPath: ".yogourt/api/health/route.go.so"}

	got := pluginBuildArgs("darwin", Config{}, source)
	want := []string{"build", "-buildmode=plugin", "-ldflags=-w", "-o", source.PluginPath, source.Path}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("pluginBuildArgs(darwin) = %v, want %v", got, want)
	}
}

func TestPluginBuildArgsKeepDwarfElsewhere(t *testing.T) {
	source := Source{Path: "api/health/route.go", PluginPath: ".yogourt/api/health/route.go.so"}

	got := pluginBuildArgs("linux", Config{}, source)
	want := []string{"build", "-buildmode=plugin", "-o", source.PluginPath, source.Path}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("pluginBuildArgs(linux) = %v, want %v", got, want)
	}
}

func TestPluginBuildArgsAppendConfiguredFlags(t *testing.T) {
	source := Source{Path: "api/health/route.go", PluginPath: ".yogourt/api/health/route.go.so"}
	cfg := Config{BuildFlags: []string{"-tags=integration", "-trimpath"}}

	got := pluginBuildArgs("darwin", cfg, source)
	want := []string{"build", "-buildmode=plugin", "-ldflags=-w", "-tags=integration", "-trimpath", "-o", source.PluginPath, source.Path}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("pluginBuildArgs(darwin, flags) = %v, want %v", got, want)
	}
}

func TestPluginBuildArgsLetConfiguredLinkerFlagsWin(t *testing.T) {
	source := Source{Path: "api/health/route.go", PluginPath: ".yogourt/api/health/route.go.so"}

	for _, flags := range [][]string{{"-ldflags=-s -w"}, {"-ldflags", "-X main.version=1"}} {
		got := pluginBuildArgs("darwin", Config{BuildFlags: flags}, source)
		want := append([]string{"build", "-buildmode=plugin"}, flags...)
		want = append(want, "-o", source.PluginPath, source.Path)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("pluginBuildArgs(darwin, %v) = %v, want %v", flags, got, want)
		}
	}
}
