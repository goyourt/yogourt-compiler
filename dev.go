package compiler

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"time"
)

type FileState struct {
	ModTime time.Time
	Size    int64
}

type RuntimeProcess struct {
	cmd  *exec.Cmd
	done chan error
}

func Dev(ctx context.Context, cfg Config) error {
	cfg, err := cfg.normalize()
	if err != nil {
		return err
	}
	if len(cfg.RuntimeCommand) == 0 {
		return errors.New("runtime command is required")
	}

	ctx, stop := signal.NotifyContext(ctx, runtimeStopSignals()...)
	defer stop()

	snapshot, err := Snapshot(cfg)
	if err != nil {
		return err
	}

	var process *RuntimeProcess
	if cfg.NoInitialBuild {
		cfg.Logger.Println("skipping initial plugin build")
		process, err = startRuntime(cfg)
	} else {
		process, err = rebuildAndStart(ctx, cfg)
	}
	if err != nil {
		return err
	}
	defer func() {
		if process != nil {
			if err := stopRuntime(cfg, process); err != nil {
				cfg.Logger.Printf("unable to stop runtime cleanly: %v", err)
			}
		}
	}()

	ticker := time.NewTicker(cfg.PollInterval)
	defer ticker.Stop()

	for {
		if process != nil {
			select {
			case err := <-process.done:
				cfg.Logger.Printf("runtime stopped: %v", err)
				process = nil
			default:
			}
		}

		select {
		case <-ctx.Done():
			if process != nil {
				if err := stopRuntime(cfg, process); err != nil {
					cfg.Logger.Printf("unable to stop runtime cleanly: %v", err)
				}
				process = nil
			}
			return nil
		case <-ticker.C:
			nextSnapshot, changedFiles, err := ChangedFiles(cfg, snapshot)
			if err != nil {
				cfg.Logger.Printf("watch error: %v", err)
				continue
			}
			if len(changedFiles) == 0 {
				continue
			}
			snapshot = nextSnapshot

			cfg.Logger.Printf("change detected; rebuilding plugins for %d changed file(s)", len(changedFiles))
			for _, changedFile := range changedFiles {
				cfg.Logger.Printf("changed: %s", changedFile)
			}
			sources, fullRebuild, err := SourcesForChangedFiles(cfg, changedFiles)
			if err != nil {
				cfg.Logger.Printf("unable to resolve changed sources: %v", err)
				continue
			}
			if fullRebuild {
				cfg.Logger.Println("shared file changed; rebuilding all plugins")
			}

			var report BuildReport
			if fullRebuild {
				report, err = Build(ctx, cfg)
			} else {
				report = BuildSources(ctx, cfg, sources)
			}
			if err != nil {
				cfg.Logger.Printf("build failed: %v", err)
				continue
			}

			logReport(cfg, report)
			if !report.Success {
				cfg.Logger.Println("build failed; keeping current runtime alive")
				continue
			}

			if process != nil {
				if err := stopRuntime(cfg, process); err != nil {
					cfg.Logger.Printf("unable to stop runtime cleanly: %v", err)
				}
				process = nil
			}

			process, err = startRuntime(cfg)
			if err != nil {
				cfg.Logger.Printf("unable to restart runtime: %v", err)
			}
		}
	}
}

func rebuildAndStart(ctx context.Context, cfg Config) (*RuntimeProcess, error) {
	report, err := Build(ctx, cfg)
	if err != nil {
		return nil, err
	}
	logReport(cfg, report)
	if !report.Success {
		return nil, errors.New("initial plugin build failed")
	}

	return startRuntime(cfg)
}

func startRuntime(cfg Config) (*RuntimeProcess, error) {
	cmd := exec.Command(cfg.RuntimeCommand[0], cfg.RuntimeCommand[1:]...)
	cmd.Dir = cfg.Root
	cmd.Stdout = cfg.Stdout
	cmd.Stderr = cfg.Stderr
	cmd.Stdin = cfg.Stdin
	prepareRuntimeCommand(cmd)

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	process := &RuntimeProcess{
		cmd:  cmd,
		done: make(chan error, 1),
	}

	go func() {
		process.done <- cmd.Wait()
	}()

	cfg.Logger.Printf("runtime started pid=%d", cmd.Process.Pid)
	return process, nil
}

func stopRuntime(cfg Config, process *RuntimeProcess) error {
	if process == nil || process.cmd == nil || process.cmd.Process == nil {
		return nil
	}

	if err := interruptRuntimeProcess(process.cmd); err != nil {
		_ = killRuntimeProcess(process.cmd)
		return err
	}

	select {
	case <-process.done:
		return nil
	case <-time.After(cfg.RestartDelay):
		if err := killRuntimeProcess(process.cmd); err != nil {
			return err
		}
		<-process.done
		return nil
	}
}

func Snapshot(cfg Config) (map[string]FileState, error) {
	cfg, err := cfg.normalize()
	if err != nil {
		return nil, err
	}

	state := make(map[string]FileState)
	for _, watchPath := range cfg.WatchPaths {
		root := filepath.Join(cfg.Root, filepath.FromSlash(watchPath))
		if err := addSnapshotPath(cfg, state, root); err != nil {
			return nil, err
		}
	}
	return state, nil
}

func Changed(cfg Config, previous map[string]FileState) (map[string]FileState, bool, error) {
	current, changedFiles, err := ChangedFiles(cfg, previous)
	return current, len(changedFiles) > 0, err
}

func ChangedFiles(cfg Config, previous map[string]FileState) (map[string]FileState, []string, error) {
	current, err := Snapshot(cfg)
	if err != nil {
		return nil, nil, err
	}

	changed := make([]string, 0)

	for path, currentState := range current {
		previousState, ok := previous[path]
		if !ok {
			changed = append(changed, path)
			continue
		}
		if !currentState.ModTime.Equal(previousState.ModTime) || currentState.Size != previousState.Size {
			changed = append(changed, path)
		}
	}

	for path := range previous {
		if _, ok := current[path]; !ok {
			changed = append(changed, path)
		}
	}

	return current, changed, nil
}

func addSnapshotPath(cfg Config, state map[string]FileState, path string) error {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	if !info.IsDir() {
		if isWatchedFile(path) {
			state[path] = FileState{ModTime: info.ModTime(), Size: info.Size()}
		}
		return nil
	}

	return filepath.WalkDir(path, func(current string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if current != path && ignoredDir(cfg, d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !isWatchedFile(current) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		state[current] = FileState{ModTime: info.ModTime(), Size: info.Size()}
		return nil
	})
}

func ignoredDir(cfg Config, name string) bool {
	for _, ignored := range cfg.IgnoreDirs {
		if name == ignored {
			return true
		}
	}
	return false
}

func isWatchedFile(path string) bool {
	name := filepath.Base(path)
	if strings.HasSuffix(name, "_test.go") {
		return false
	}
	if strings.HasSuffix(name, ".go") {
		return true
	}
	return name == "go.mod" || name == "go.sum"
}

func logReport(cfg Config, report BuildReport) {
	for _, result := range report.Results {
		if result.Error != nil {
			cfg.Logger.Printf("❌ %s: %v", result.Source, result.Error)
			continue
		}
		cfg.Logger.Printf("✅ %s -> %s (%dms)", result.Source, result.Plugin, result.DurationMS)
	}
	cfg.Logger.Printf("build finished success=%v duration=%s", report.Success, report.FinishedAt.Sub(report.StartedAt))
}

func RuntimeCommand(command string, args ...string) []string {
	return append([]string{command}, args...)
}

func GoRunCommand(args ...string) []string {
	if len(args) == 0 {
		args = []string{"."}
	}
	return append([]string{"go", "run"}, args...)
}

func ValidateRuntimeCommand(command []string) error {
	if len(command) == 0 {
		return errors.New("runtime command is required")
	}
	if strings.TrimSpace(command[0]) == "" {
		return fmt.Errorf("runtime executable is empty")
	}
	return nil
}
