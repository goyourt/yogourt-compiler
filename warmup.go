package compiler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

const (
	cacheWarmupFolder      = "cache-warmup"
	cacheWarmupSource      = "main.go"
	cacheWarmupEnvironment = "environment"
)

type warmupCompiler func(ctx context.Context, cfg Config, warmup Source) error

func warmDependencyCache(ctx context.Context, cfg Config, sources []Source) {
	warmDependencyCacheWith(ctx, cfg, sources, compileWarmup)
}

func warmDependencyCacheWith(ctx context.Context, cfg Config, sources []Source, compile warmupCompiler) {
	imports := sourceImports(sources)
	if len(imports) == 0 {
		return
	}

	warmupDir := filepath.Join(cfg.Root, filepath.FromSlash(outputFolderOrDefault(cfg)), cacheWarmupFolder)
	warmup := Source{
		Path:       filepath.Join(warmupDir, cacheWarmupSource),
		PluginPath: filepath.Join(warmupDir, cacheWarmupSource+".so"),
	}
	environmentPath := filepath.Join(warmupDir, cacheWarmupEnvironment)
	environment := buildEnvironmentFingerprint(ctx, cfg)

	if storedEnvironment, err := os.ReadFile(environmentPath); err == nil && string(storedEnvironment) == environment {
		if containsAll(sourceImports([]Source{warmup}), imports) {
			return
		}
	}

	if err := os.MkdirAll(warmupDir, 0o755); err != nil {
		return
	}
	_ = os.Remove(environmentPath)
	if err := os.WriteFile(warmup.Path, []byte(warmupProgram(imports)), 0o644); err != nil {
		return
	}

	err := compile(ctx, cfg, warmup)
	_ = os.Remove(warmup.PluginPath)
	if err != nil {
		return
	}
	_ = os.WriteFile(environmentPath, []byte(environment), 0o644)
}

func compileWarmup(ctx context.Context, cfg Config, warmup Source) error {
	cmd := exec.CommandContext(ctx, "go", pluginBuildArgs(runtime.GOOS, cfg, warmup)...)
	cmd.Dir = cfg.Root
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Run()
}

func outputFolderOrDefault(cfg Config) string {
	if cfg.OutputFolder == "" {
		return DefaultOutputFolder
	}
	return cfg.OutputFolder
}

func buildEnvironmentFingerprint(ctx context.Context, cfg Config) string {
	hash := sha256.New()
	writePart := func(part []byte) {
		hash.Write([]byte(strconv.Itoa(len(part)) + ":"))
		hash.Write(part)
	}

	for _, name := range []string{"go.mod", "go.sum"} {
		content, _ := os.ReadFile(filepath.Join(cfg.Root, name))
		writePart(content)
	}

	goEnv := exec.CommandContext(ctx, "go", "env", "GOVERSION", "GOFLAGS", "GOCACHE", "GOARCH", "CGO_ENABLED")
	goEnv.Dir = cfg.Root
	toolchain, _ := goEnv.Output()
	writePart(toolchain)

	flags := pluginBuildArgs(runtime.GOOS, cfg, Source{})
	writePart([]byte(strings.Join(flags, "\x00")))

	return hex.EncodeToString(hash.Sum(nil))
}

func containsAll(available []string, wanted []string) bool {
	known := make(map[string]struct{}, len(available))
	for _, importPath := range available {
		known[importPath] = struct{}{}
	}
	for _, importPath := range wanted {
		if _, ok := known[importPath]; !ok {
			return false
		}
	}
	return true
}

func sourceImports(sources []Source) []string {
	seen := make(map[string]struct{})
	fileSet := token.NewFileSet()

	for _, source := range sources {
		file, err := parser.ParseFile(fileSet, source.Path, nil, parser.ImportsOnly)
		if err != nil {
			continue
		}
		for _, spec := range file.Imports {
			importPath, err := strconv.Unquote(spec.Path.Value)
			if err != nil || importPath == "C" {
				continue
			}
			seen[importPath] = struct{}{}
		}
	}

	imports := make([]string, 0, len(seen))
	for importPath := range seen {
		imports = append(imports, importPath)
	}
	sort.Strings(imports)
	return imports
}

func warmupProgram(imports []string) string {
	var program strings.Builder
	program.WriteString("package main\n\nimport (\n")
	for _, importPath := range imports {
		program.WriteString("\t_ " + strconv.Quote(importPath) + "\n")
	}
	program.WriteString(")\n\nfunc main() {}\n")
	return program.String()
}
