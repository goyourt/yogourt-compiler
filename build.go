package compiler

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

type Source struct {
	Path       string
	PluginPath string
}

type BuildResult struct {
	Source     string
	Plugin     string
	Error      error
	DurationMS int64
}

type BuildReport struct {
	StartedAt  time.Time
	FinishedAt time.Time
	Success    bool
	Results    []BuildResult
}

func Build(ctx context.Context, cfg Config) (BuildReport, error) {
	cfg, err := cfg.normalize()
	if err != nil {
		return BuildReport{}, err
	}

	sources, err := DiscoverSources(cfg)
	if err != nil {
		return BuildReport{}, err
	}

	return BuildSources(ctx, cfg, sources), nil
}

func BuildSources(ctx context.Context, cfg Config, sources []Source) BuildReport {
	startedAt := time.Now()
	results := make([]BuildResult, len(sources))
	var wg sync.WaitGroup

	maxWorkers := runtime.NumCPU() / 2
	if maxWorkers < 1 {
		maxWorkers = 1
	}
	sem := make(chan struct{}, maxWorkers)

	for i, source := range sources {
		if ctx.Err() != nil {
			results[i] = BuildResult{
				Source: source.Path,
				Plugin: source.PluginPath,
				Error:  ctx.Err(),
			}
			continue
		}

		wg.Add(1)
		sem <- struct{}{}
		go func(index int, src Source) {
			defer wg.Done()
			defer func() { <-sem }()
			results[index] = BuildPlugin(ctx, cfg, src)
		}(i, source)
	}

	wg.Wait()

	success := true
	for _, result := range results {
		if result.Error != nil {
			success = false
			break
		}
	}

	return BuildReport{
		StartedAt:  startedAt,
		FinishedAt: time.Now(),
		Success:    success,
		Results:    results,
	}
}

func BuildPlugin(ctx context.Context, cfg Config, source Source) BuildResult {
	startedAt := time.Now()
	result := BuildResult{
		Source: source.Path,
		Plugin: source.PluginPath,
	}

	if err := os.MkdirAll(filepath.Dir(source.PluginPath), 0o755); err != nil {
		result.Error = err
		result.DurationMS = time.Since(startedAt).Milliseconds()
		return result
	}

	cmd := exec.CommandContext(
		ctx,
		"go", "build",
		"-buildmode=plugin",
		"-o", source.PluginPath,
		source.Path,
	)
	cmd.Dir = cfg.Root
	cmd.Stdout = cfg.Stdout
	cmd.Stderr = cfg.Stderr

	if err := cmd.Run(); err != nil {
		result.Error = fmt.Errorf("build plugin: %w", err)
	}
	result.DurationMS = time.Since(startedAt).Milliseconds()
	return result
}

func DiscoverSources(cfg Config) ([]Source, error) {
	cfg, err := cfg.normalize()
	if err != nil {
		return nil, err
	}

	sources := make([]Source, 0)

	middlewarePath := filepath.Join(cfg.Root, filepath.FromSlash(cfg.MiddlewareFile))
	if source, ok, err := sourceIfExists(cfg, middlewarePath); err != nil {
		return nil, err
	} else if ok {
		sources = append(sources, source)
	}

	routeRoot := filepath.Join(cfg.Root, filepath.FromSlash(cfg.RouteFolder))
	if _, err := os.Stat(routeRoot); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("route folder not found: %s", routeRoot)
		}
		return nil, err
	}

	err = filepath.WalkDir(routeRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !isPluginSource(d.Name()) {
			return nil
		}

		source, ok, err := sourceIfExists(cfg, path)
		if err != nil {
			return err
		}
		if ok {
			sources = append(sources, source)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	if len(sources) == 0 {
		return nil, errors.New("no Yogourt plugin source found")
	}

	return sources, nil
}

func SourcesForChangedFiles(cfg Config, changedFiles []string) ([]Source, bool, error) {
	cfg, err := cfg.normalize()
	if err != nil {
		return nil, false, err
	}

	sources := make([]Source, 0, len(changedFiles))
	seen := make(map[string]struct{})

	for _, changedFile := range changedFiles {
		if !isPluginSource(filepath.Base(changedFile)) {
			return nil, true, nil
		}

		absChangedFile, err := filepath.Abs(changedFile)
		if err != nil {
			return nil, false, err
		}

		if isMiddlewareSource(cfg, absChangedFile) || isRouteSource(cfg, absChangedFile) {
			source, ok, err := sourceIfExists(cfg, absChangedFile)
			if err != nil {
				return nil, false, err
			}
			if !ok {
				return nil, true, nil
			}
			if _, exists := seen[source.Path]; !exists {
				sources = append(sources, source)
				seen[source.Path] = struct{}{}
			}
			continue
		}

		return nil, true, nil
	}

	if len(sources) == 0 {
		return nil, true, nil
	}

	return sources, false, nil
}

func sourceIfExists(cfg Config, path string) (Source, bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Source{}, false, nil
		}
		return Source{}, false, err
	}
	if info.IsDir() || !isPluginSource(info.Name()) {
		return Source{}, false, nil
	}

	pluginPath, err := PluginPath(cfg, path)
	if err != nil {
		return Source{}, false, err
	}

	return Source{
		Path:       path,
		PluginPath: pluginPath,
	}, true, nil
}

func isMiddlewareSource(cfg Config, path string) bool {
	middlewarePath := filepath.Join(cfg.Root, filepath.FromSlash(cfg.MiddlewareFile))
	absMiddlewarePath, err := filepath.Abs(middlewarePath)
	if err != nil {
		return false
	}
	return path == absMiddlewarePath
}

func isRouteSource(cfg Config, path string) bool {
	routeRoot := filepath.Join(cfg.Root, filepath.FromSlash(cfg.RouteFolder))
	absRouteRoot, err := filepath.Abs(routeRoot)
	if err != nil {
		return false
	}

	rel, err := filepath.Rel(absRouteRoot, path)
	if err != nil {
		return false
	}

	return rel != "." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".."
}

func PluginPath(cfg Config, sourcePath string) (string, error) {
	cfg, err := cfg.normalize()
	if err != nil {
		return "", err
	}

	rel, err := filepath.Rel(cfg.Root, sourcePath)
	if err != nil {
		return "", err
	}

	return filepath.Join(cfg.Root, filepath.FromSlash(cfg.OutputFolder), rel+".so"), nil
}

func isPluginSource(name string) bool {
	return strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go")
}
