# Yogourt Compiler

Development compiler/supervisor for Yogourt projects.

This module is intended to be used by `yogourt-cli`, especially by:

```sh
yogourt dev
```

It should not require any change in the user's Yogourt application code.

## Responsibilities

- discover Yogourt plugin sources:
  - `api/**/*.go`
  - `middleware/middleware.go`
- compile plugins to `.yogourt/**/*.so`
- start the Yogourt web runtime command
- watch project changes during development
- rebuild plugins on change
- restart the web runtime only when rebuild succeeds

## Non-responsibilities

- the Yogourt runtime should not compile plugins
- the Yogourt application code should not import this module
- production runtime should only load already compiled plugins

## CLI integration example

```go
package dev

import (
	"context"

	yogourtcompiler "github.com/goyourt/yogourt-compiler"
)

func RunDev(ctx context.Context) error {
	return yogourtcompiler.Dev(ctx, yogourtcompiler.Config{
		Root:           ".",
		RouteFolder:    "api",
		MiddlewareFile: "middleware/middleware.go",
		RuntimeCommand: yogourtcompiler.GoRunCommand("."),
	})
}
```

To skip the initial full plugin build and start the runtime immediately:

```sh
yogourt dev --no-initial-build
```

This is faster, but existing `.so` plugins must already be compatible with the current Go toolchain and dependencies.

## Build only

For a future `yogourt build` command:

```go
report, err := yogourtcompiler.Build(ctx, yogourtcompiler.Config{
	Root: ".",
})
```

## Watch behavior

The default watcher uses polling for portability.

It watches Go module files and Go source files, while ignoring folders such as:

- `.git`
- `.yogourt`
- `vendor`
- `node_modules`
- `public`
- `var`
- `tmp`

For now, any relevant change rebuilds all Yogourt plugins. Dependency-aware partial rebuilds can be added later.
