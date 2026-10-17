package compiler

import (
	"reflect"
	"testing"
)

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
