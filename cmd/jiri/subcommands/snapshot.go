// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package subcommands

import (
	"context"
	"flag"

	"github.com/google/subcommands"
	"go.fuchsia.dev/jiri"
	"go.fuchsia.dev/jiri/project"
)

// TODO(https://fxbug.dev/356134056): delete when finished migrating to
// subcommands library.
var (
	snapshotFlags snapshotCmd
	cmdSnapshot   = commandFromSubcommand(&snapshotFlags)
)

// TODO(https://fxbug.dev/356134056): delete when finished migrating to
// subcommands library.
func init() {
	snapshotFlags.SetFlags(&cmdSnapshot.Flags)
}

type snapshotCmd struct {
	cmdBase

	cipdEnsure bool
}

func (c *snapshotCmd) Name() string     { return "snapshot" }
func (c *snapshotCmd) Synopsis() string { return "Create a new project snapshot" }
func (c *snapshotCmd) Usage() string {
	return `
The "jiri snapshot <snapshot>" command captures the current project state
in a manifest.

Usage:
  jiri snapshot [flags] <snapshot>

<snapshot> is the snapshot manifest file.
`
}

func (c *snapshotCmd) SetFlags(f *flag.FlagSet) {
	f.BoolVar(&c.cipdEnsure, "cipd", false, "Generate a cipd.ensure (packages only) snapshot.")
}

func (c *snapshotCmd) Execute(ctx context.Context, f *flag.FlagSet, _ ...any) subcommands.ExitStatus {
	return executeWrapper(ctx, c.run, c.topLevelFlags, f.Args())
}

func (c *snapshotCmd) run(jirix *jiri.X, args []string) error {
	if len(args) != 1 {
		return jirix.UsageErrorf("unexpected number of arguments")
	}
	return project.CreateSnapshot(jirix, args[0], nil, nil, true, c.cipdEnsure)
}
