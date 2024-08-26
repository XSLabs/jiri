// Copyright 2022 The Fuchsia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package subcommands

import (
	"context"
	"flag"
	"fmt"
	"path/filepath"
	"sort"

	"github.com/google/subcommands"
	"go.fuchsia.dev/jiri"
	"go.fuchsia.dev/jiri/gitutil"
	"go.fuchsia.dev/jiri/project"
)

type checkCleanCmd struct {
	cmdBase
}

func (c *checkCleanCmd) Name() string     { return "check-clean" }
func (c *checkCleanCmd) Synopsis() string { return "Checks if the checkout is clean" }
func (c *checkCleanCmd) Usage() string {
	return `Exits non-zero and prints repositories (and their status) if they contain
uncommitted changes.

Usage:
  jiri check-clean
`
}

func (c *checkCleanCmd) SetFlags(f *flag.FlagSet) {}

func (c *checkCleanCmd) Execute(ctx context.Context, f *flag.FlagSet, _ ...any) subcommands.ExitStatus {
	return executeWrapper(ctx, c.run, c.topLevelFlags, f.Args())
}

func (c *checkCleanCmd) run(jirix *jiri.X, args []string) error {
	localProjects, err := project.LocalProjects(jirix, project.FastScan)
	if err != nil {
		return err
	}
	cDir := jirix.Cwd
	var keys project.ProjectKeys
	for key := range localProjects {
		keys = append(keys, key)
	}
	sort.Sort(keys)
	dirtyProjects := make(map[string]string)
	for _, key := range keys {
		localProject := localProjects[key]
		relativePath, err := filepath.Rel(cDir, localProject.Path)
		if err != nil {
			return err
		}
		scm := gitutil.New(jirix, gitutil.RootDirOpt(localProject.Path))
		changes, err := scm.ShortStatus()
		if err != nil {
			jirix.Logger.Errorf("%s :%s\n\n", fmt.Sprintf("getting changes for project %s(%s)", localProject.Name, relativePath), err)
			jirix.IncrementFailures()
			continue
		}
		if changes != "" {
			dirtyProjects[relativePath] = changes
		}
	}
	var finalErr error
	if jirix.Failures() != 0 {
		finalErr = fmt.Errorf("completed with non-fatal errors")
	} else if len(dirtyProjects) > 0 {
		finalErr = fmt.Errorf("Checkout is not clean!")
	}

	if len(dirtyProjects) > 0 {
		fmt.Fprintln(jirix.Stdout(), "Dirty projects:")
		for relativePath, changes := range dirtyProjects {
			fmt.Fprintf(jirix.Stdout(), "%s\n%s\n\n", relativePath, changes)
		}
	}

	return finalErr
}
