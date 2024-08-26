// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cmdline

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/google/subcommands"
	_ "go.fuchsia.dev/jiri/metadata" // for the -metadata flag
	"go.fuchsia.dev/jiri/timing"
)

// Main implements the main function for the given commander.
func Main(env *Env, commander *subcommands.Commander) subcommands.ExitStatus {
	if env.Timer != nil && len(env.Timer.Intervals) > 0 {
		env.Timer.Intervals[0].Name = commander.Name()
	}
	ctx := AddEnvToContext(context.Background(), env)
	var flagTime bool
	var flagTimeFile string
	// Hack to get around the fact that we can't import the flagTime and
	// flagTimeFile variables into this package as that would cause circular
	// imports.
	commander.VisitAll(func(f *flag.Flag) {
		switch f.Name {
		case "time":
			flagTime, _ = strconv.ParseBool(f.Value.String())
		case "timefile":
			flagTimeFile = f.Value.String()
		}
	})
	code := commander.Execute(ctx)
	if err := writeTiming(env, flagTime, flagTimeFile); err != nil {
		if code == 0 {
			code = subcommands.ExitStatus(ExitCode(err, env.Stderr))
		}
	}
	return code
}

func writeTiming(env *Env, timingEnabled bool, timeFile string) error {
	if !timingEnabled || env.Timer == nil {
		return nil
	}

	env.Timer.Finish()
	p := timing.IntervalPrinter{Zero: env.Timer.Zero}
	w := env.Stderr
	var cleanup func() error
	if timeFile != "" {
		f, openErr := os.OpenFile(timeFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
		if openErr != nil {
			return openErr
		}
		w = f
		cleanup = f.Close
	}
	err := p.Print(w, env.Timer.Intervals, env.Timer.Now())
	if cleanup != nil {
		if err2 := cleanup(); err == nil {
			err = err2
		}
	}
	return err
}

// ErrExitCode may be returned by Runner.Run to cause the program to exit with a
// specific error code.
type ErrExitCode int

// Error implements the error interface method.
func (x ErrExitCode) Error() string {
	return fmt.Sprintf("exit code %d", x)
}

// ErrUsage indicates an error in command usage; e.g. unknown flags, subcommands
// or args.  It corresponds to exit code 2.
const ErrUsage = ErrExitCode(2)

// ExitCode returns the exit code corresponding to err.
//
//	0:    if err == nil
//	code: if err is ErrExitCode(code)
//	1:    all other errors
//
// Writes the error message for "all other errors" to w, if w is non-nil.
func ExitCode(err error, w io.Writer) subcommands.ExitStatus {
	if err == nil {
		return 0
	}
	if code, ok := err.(ErrExitCode); ok {
		return subcommands.ExitStatus(code)
	}
	if w != nil {
		// We don't print "ERROR: exit code N" above to avoid cluttering the output.
		fmt.Fprintf(w, "ERROR: %v\n", err)
	}
	return 1
}
