package compiler

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

type warmupProject struct {
	t        *testing.T
	cfg      Config
	compiled int
	failure  error
}

func newWarmupProject(t *testing.T) *warmupProject {
	t.Helper()
	root := t.TempDir()
	project := &warmupProject{t: t, cfg: Config{Root: root, OutputFolder: DefaultOutputFolder}}
	project.write("go.mod", "module example.com/app\n\ngo 1.24\n")
	project.write("go.sum", "example.com/dep v1.0.0 h1:first\n")
	return project
}

func (project *warmupProject) write(name string, content string) string {
	project.t.Helper()
	path := filepath.Join(project.cfg.Root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		project.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		project.t.Fatal(err)
	}
	return path
}

func (project *warmupProject) source(name string, imports ...string) Source {
	project.t.Helper()
	return Source{Path: project.write(name, warmupProgram(imports))}
}

func (project *warmupProject) warm(sources ...Source) {
	project.t.Helper()
	compile := func(ctx context.Context, cfg Config, warmup Source) error {
		project.compiled++
		return project.failure
	}
	warmDependencyCacheWith(context.Background(), project.cfg, sources, compile)
}

func (project *warmupProject) storedImports() []string {
	path := filepath.Join(project.cfg.Root, DefaultOutputFolder, cacheWarmupFolder, cacheWarmupSource)
	return sourceImports([]Source{{Path: path}})
}

func TestWarmupCompilesOnceWhileDependenciesAreUnchanged(t *testing.T) {
	project := newWarmupProject(t)
	health := project.source("api/health/route.go", "fmt", "example.com/dep")
	users := project.source("api/users/route.go", "example.com/dep/sub")

	project.warm(health, users)
	project.warm(health, users)

	if project.compiled != 1 {
		t.Fatalf("warmup compiled %d times, want 1", project.compiled)
	}
	want := []string{"example.com/dep", "example.com/dep/sub", "fmt"}
	if got := project.storedImports(); !reflect.DeepEqual(got, want) {
		t.Fatalf("stored imports = %v, want %v", got, want)
	}
}

func TestWarmupSkipsSourcesImportingASubsetOfTheStoredDependencies(t *testing.T) {
	project := newWarmupProject(t)
	health := project.source("api/health/route.go", "fmt", "example.com/dep")
	users := project.source("api/users/route.go", "example.com/dep/sub")

	project.warm(health, users)
	project.warm(health)

	if project.compiled != 1 {
		t.Fatalf("warmup compiled %d times, want 1", project.compiled)
	}
	if got := project.storedImports(); len(got) != 3 {
		t.Fatalf("stored imports = %v, want the 3 imports of the full build", got)
	}
}

func TestWarmupRecompilesWhenASourceImportsANewDependency(t *testing.T) {
	project := newWarmupProject(t)
	health := project.source("api/health/route.go", "fmt")

	project.warm(health)
	health = project.source("api/health/route.go", "fmt", "example.com/dep")
	project.warm(health)

	if project.compiled != 2 {
		t.Fatalf("warmup compiled %d times, want 2", project.compiled)
	}
}

func TestWarmupRecompilesWhenModuleDependenciesChange(t *testing.T) {
	project := newWarmupProject(t)
	health := project.source("api/health/route.go", "example.com/dep")

	project.warm(health)
	project.write("go.sum", "example.com/dep v1.1.0 h1:second\n")
	project.warm(health)

	if project.compiled != 2 {
		t.Fatalf("warmup compiled %d times, want 2", project.compiled)
	}
}

func TestWarmupRecompilesWhenBuildFlagsChange(t *testing.T) {
	project := newWarmupProject(t)
	health := project.source("api/health/route.go", "example.com/dep")

	project.warm(health)
	project.cfg.BuildFlags = []string{"-tags=integration"}
	project.warm(health)

	if project.compiled != 2 {
		t.Fatalf("warmup compiled %d times, want 2", project.compiled)
	}
}

func TestWarmupRetriesAfterAFailedCompilation(t *testing.T) {
	project := newWarmupProject(t)
	health := project.source("api/health/route.go", "example.com/dep")

	project.failure = errors.New("boom")
	project.warm(health)
	project.failure = nil
	project.warm(health)
	project.warm(health)

	if project.compiled != 2 {
		t.Fatalf("warmup compiled %d times, want 2", project.compiled)
	}
}

func TestWarmupLeavesNoPluginBehind(t *testing.T) {
	project := newWarmupProject(t)
	health := project.source("api/health/route.go", "fmt")
	compile := func(ctx context.Context, cfg Config, warmup Source) error {
		return os.WriteFile(warmup.PluginPath, []byte("plugin"), 0o644)
	}

	warmDependencyCacheWith(context.Background(), project.cfg, []Source{health}, compile)

	matches, err := filepath.Glob(filepath.Join(project.cfg.Root, DefaultOutputFolder, cacheWarmupFolder, "*.so"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("warmup left plugins behind: %v", matches)
	}
}
