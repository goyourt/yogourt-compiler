package compiler

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"time"
)

const (
	DefaultRouteFolder    = "api"
	DefaultMiddlewareFile = "middleware/middleware.go"
	DefaultOutputFolder   = ".yogourt"
)

type Config struct {
	Root           string
	RouteFolder    string
	MiddlewareFile string
	OutputFolder   string
	RuntimeCommand []string
	NoInitialBuild bool
	WatchPaths     []string
	IgnoreDirs     []string
	PollInterval   time.Duration
	RestartDelay   time.Duration
	MaxWorkers     int
	BuildFlags     []string
	Stdout         io.Writer
	Stderr         io.Writer
	Stdin          io.Reader
	Logger         *log.Logger
}

func (cfg Config) normalize() (Config, error) {
	if cfg.Root == "" {
		wd, err := os.Getwd()
		if err != nil {
			return cfg, err
		}
		cfg.Root = wd
	}

	root, err := filepath.Abs(cfg.Root)
	if err != nil {
		return cfg, err
	}
	cfg.Root = root

	if cfg.RouteFolder == "" {
		cfg.RouteFolder = DefaultRouteFolder
	}
	if cfg.MiddlewareFile == "" {
		cfg.MiddlewareFile = DefaultMiddlewareFile
	}
	if cfg.OutputFolder == "" {
		cfg.OutputFolder = DefaultOutputFolder
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 500 * time.Millisecond
	}
	if cfg.RestartDelay < 0 {
		cfg.RestartDelay = 0
	}
	if cfg.Stdout == nil {
		cfg.Stdout = os.Stdout
	}
	if cfg.Stderr == nil {
		cfg.Stderr = os.Stderr
	}
	if cfg.Stdin == nil {
		cfg.Stdin = os.Stdin
	}
	if cfg.Logger == nil {
		cfg.Logger = log.New(os.Stdout, "[yogourt-compiler] ", log.LstdFlags)
	}
	if len(cfg.WatchPaths) == 0 {
		cfg.WatchPaths = []string{"."}
	}
	if len(cfg.IgnoreDirs) == 0 {
		cfg.IgnoreDirs = []string{
			".git",
			".yogourt",
			"node_modules",
			"vendor",
			"public",
			"var",
			"tmp",
		}
	}

	return cfg, nil
}
