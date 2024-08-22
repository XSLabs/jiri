// Copyright 2017 The Fuchsia Authors. All rights reserved.
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
	cmdSourceManifest = commandFromSubcommand(&sourceManifestCmd{})
)

type sourceManifestCmd struct {
	cmdBase
}

func (c *sourceManifestCmd) Name() string { return "source-manifest" }
func (c *sourceManifestCmd) Synopsis() string {
	return "Create a new source-manifest from current checkout"
}
func (c *sourceManifestCmd) Usage() string {
	return `
This command captures the current project state in a source-manifest format.

Usage:
  jiri source-manifest <source-manifest>

<source-manifest> is the source-manifest file.
`
}

func (c *sourceManifestCmd) SetFlags(f *flag.FlagSet) {}

func (c *sourceManifestCmd) Execute(ctx context.Context, f *flag.FlagSet, _ ...any) subcommands.ExitStatus {
	return executeWrapper(ctx, c.run, c.topLevelFlags, f.Args())
}

func (c *sourceManifestCmd) run(jirix *jiri.X, args []string) error {
	jirix.TimerPush("create source manifest")
	defer jirix.TimerPop()

	if len(args) != 1 {
		return jirix.UsageErrorf("unexpected number of arguments")
	}

	localProjects, err := project.LocalProjects(jirix, project.FullScan)
	if err != nil {
		return err
	}

	sm, err := project.NewSourceManifest(jirix, localProjects)
	if err != nil {
		return err
	}
	return sm.ToFile(jirix, args[0])
}
