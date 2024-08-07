// Copyright 2017 The Fuchsia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package subcommands

import (
	"context"
	"flag"
	"fmt"

	"github.com/google/subcommands"
	"go.fuchsia.dev/jiri"
	"go.fuchsia.dev/jiri/cmdline"
)

// TODO(https://fxbug.dev/356134056): delete when finished migrating to
// subcommands library.
var (
	selfUpdateFlags selfUpdateCmd
	cmdSelfUpdate   = &cmdline.Command{
		Runner: cmdline.RunnerFunc(selfUpdateFlags.run),
		Name:   selfUpdateFlags.Name(),
		Short:  selfUpdateFlags.Synopsis(),
		Long:   selfUpdateFlags.Usage(),
	}
)

type selfUpdateCmd struct{}

func (c *selfUpdateCmd) Name() string     { return "selfupdate" }
func (c *selfUpdateCmd) Synopsis() string { return "Update jiri tool" }
func (c *selfUpdateCmd) Usage() string {
	return `
Updates jiri tool and replaces current one with the latest

Usage:
  jiri selfupdate
`
}

func (c *selfUpdateCmd) SetFlags(f *flag.FlagSet) {}

func (c *selfUpdateCmd) Execute(ctx context.Context, _ *flag.FlagSet, args ...any) subcommands.ExitStatus {
	return errToExitStatus(c.run(ctx, argsToStrings(args)))
}

func (c *selfUpdateCmd) run(ctx context.Context, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("unexpected number of arguments")
	}

	if err := jiri.Update(true); err != nil {
		return fmt.Errorf("Update failed: %s", err)
	}
	fmt.Println("Tool updated.")
	return nil
}
