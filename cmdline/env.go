// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cmdline

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"

	"go.fuchsia.dev/jiri/envvar"
	"go.fuchsia.dev/jiri/textutil"
	"go.fuchsia.dev/jiri/timing"
)

type envContextKeyType string

const (
	envContextKey = envContextKeyType("env")
)

// AddEnvToContext returns a new Context with the `env` added to it.
func AddEnvToContext(ctx context.Context, env *Env) context.Context {
	return context.WithValue(ctx, envContextKey, env)
}

// EnvFromContext retrieves the Env from the given Context.
func EnvFromContext(ctx context.Context) *Env {
	if env, ok := ctx.Value(envContextKey).(*Env); ok && env != nil {
		return env
	}
	return nil
}

// EnvFromOS returns a new environment based on the operating system.
func EnvFromOS() *Env {
	return &Env{
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Vars:   envvar.SliceToMap(os.Environ()),
		Timer:  timing.NewTimer("root"),
	}
}

// Env represents the environment for command parsing and running.  Typically
// EnvFromOS is used to produce a default environment.  The environment may be
// explicitly set for finer control; e.g. in tests.
type Env struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	Vars   map[string]string // Environment variables
	Timer  *timing.Timer

	// Usage is a function that prints usage information to w.  Typically set by
	// calls to Main or Parse to print usage of the leaf command.
	Usage func(ctx context.Context, w io.Writer)

	// Comamnd name
	CommandName string
	// command flags
	CommandFlags map[string]string
}

func (e *Env) clone() *Env {
	return &Env{
		Stdin:  e.Stdin,
		Stdout: e.Stdout,
		Stderr: e.Stderr,
		Vars:   envvar.CopyMap(e.Vars),
		Usage:  e.Usage,
		Timer:  e.Timer, // use the same timer for all operations
	}
}

// UsageErrorf prints the error message represented by the printf-style format
// and args, followed by the output of the Usage function.  Returns ErrUsage to
// make it easy to use from within the Runner.Run function.
func (e *Env) UsageErrorf(format string, args ...any) error {
	ctx := AddEnvToContext(context.Background(), e)
	return usageErrorf(ctx, e.Usage, format, args...)
}

// TimerPush calls e.Timer.Push(name), only if the Timer is non-nil.
func (e *Env) TimerPush(name string) {
	if e.Timer != nil {
		e.Timer.Push(name)
	}
}

// TimerPop calls e.Timer.Pop(), only if the Timer is non-nil.
func (e *Env) TimerPop() {
	if e.Timer != nil {
		e.Timer.Pop()
	}
}

func usageErrorf(ctx context.Context, usage func(context.Context, io.Writer), format string, args ...any) error {
	env := EnvFromContext(ctx)
	fmt.Fprint(env.Stderr, "ERROR: ")
	fmt.Fprintf(env.Stderr, format, args...)
	fmt.Fprint(env.Stderr, "\n\n")
	if usage != nil {
		usage(ctx, env.Stderr)
	} else {
		fmt.Fprint(env.Stderr, "usage error\n")
	}
	return ErrUsage
}

// defaultWidth is a reasonable default for the output width in runes.
const defaultWidth = 80

func (e *Env) width() int {
	if width, err := strconv.Atoi(e.Vars["CMDLINE_WIDTH"]); err == nil && width != 0 {
		return width
	}
	if _, width, err := textutil.TerminalSize(); err == nil && width != 0 {
		return width
	}
	return defaultWidth
}

func (e *Env) style() style {
	style := styleCompact
	style.Set(e.Vars["CMDLINE_STYLE"])
	return style
}

func (e *Env) prefix() string {
	return e.Vars["CMDLINE_PREFIX"]
}

func (e *Env) firstCall() bool {
	return e.Vars["CMDLINE_FIRST_CALL"] == ""
}

// style describes the formatting style for usage descriptions.
type style int

const (
	styleCompact   style = iota // Default style, good for compact cmdline output.
	styleFull                   // Similar to compact but shows all global flags.
	styleGoDoc                  // Good for godoc processing.
	styleShortOnly              // Only output short description.
)

func (s *style) String() string {
	switch *s {
	case styleCompact:
		return "compact"
	case styleFull:
		return "full"
	case styleGoDoc:
		return "godoc"
	case styleShortOnly:
		return "shortonly"
	default:
		panic(fmt.Errorf("unhandled style %d", *s))
	}
}

// Set implements the flag.Value interface method.
func (s *style) Set(value string) error {
	switch value {
	case "compact":
		*s = styleCompact
	case "full":
		*s = styleFull
	case "godoc":
		*s = styleGoDoc
	case "shortonly":
		*s = styleShortOnly
	default:
		return fmt.Errorf("unknown style %q", value)
	}
	return nil
}
