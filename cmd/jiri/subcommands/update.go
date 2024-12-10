// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package subcommands

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/google/subcommands"
	"go.fuchsia.dev/jiri"
	"go.fuchsia.dev/jiri/project"
	"go.fuchsia.dev/jiri/retry"
)

const (
	minExecutionTimingThreshold time.Duration = time.Duration(30) * time.Minute        // 30 min
	maxExecutionTimingThreshold time.Duration = time.Duration(2) * time.Hour * 24 * 14 // 2 weeks
)

type updateCmd struct {
	cmdBase

	gc               bool
	localManifest    bool
	attempts         uint
	autoupdate       bool
	forceAutoupdate  bool
	rebaseUntracked  bool
	hookTimeout      uint
	fetchPkgsTimeout uint
	rebaseAll        bool
	rebaseCurrent    bool
	rebaseSubmodules bool
	rebaseTracked    bool
	runHooks         bool
	fetchPkgs        bool
	overrideOptional bool
	packagesToSkip   arrayFlag
}

func (c *updateCmd) SetFlags(f *flag.FlagSet) {
	f.BoolVar(&c.gc, "gc", true, "Garbage collect obsolete repositories.")
	f.BoolVar(&c.localManifest, "local-manifest", false, "Use local manifest")
	f.UintVar(&c.attempts, "attempts", 3, "Number of attempts before failing.")
	f.BoolVar(&c.autoupdate, "autoupdate", true, "Automatically update to the new version.")
	f.BoolVar(&c.forceAutoupdate, "force-autoupdate", false, "Always update to the current version.")
	f.BoolVar(&c.rebaseUntracked, "rebase-untracked", false, "Rebase untracked branches onto HEAD.")
	f.UintVar(&c.hookTimeout, "hook-timeout", project.DefaultHookTimeout, "Timeout in minutes for running the hooks operation.")
	f.UintVar(&c.fetchPkgsTimeout, "fetch-packages-timeout", project.DefaultPackageTimeout, "Timeout in minutes for fetching prebuilt packages using cipd.")
	f.BoolVar(&c.rebaseAll, "rebase-all", false, "Rebase all tracked branches. Also rebase all untracked branches if -rebase-untracked is passed")
	f.BoolVar(&c.rebaseCurrent, "rebase-current", false, "Deprecated. Implies -rebase-tracked. Would be removed in future.")
	f.BoolVar(&c.rebaseSubmodules, "rebase-submodules", false, "Rebase current tracked branches for submodules.")
	f.BoolVar(&c.rebaseTracked, "rebase-tracked", false, "Rebase current tracked branches instead of fast-forwarding them.")
	f.BoolVar(&c.runHooks, "run-hooks", true, "Run hooks after updating sources.")
	f.BoolVar(&c.fetchPkgs, "fetch-packages", true, "Use cipd to fetch packages.")
	f.BoolVar(&c.overrideOptional, "override-optional", false, "Override existing optional attributes in the snapshot file with current jiri settings")
	f.Var(&c.packagesToSkip, "package-to-skip", "Skip fetching this package. Repeatable.")
}

func (c *updateCmd) Name() string     { return "update" }
func (c *updateCmd) Synopsis() string { return "Update all jiri projects" }
func (c *updateCmd) Usage() string {
	return `Updates all projects. The sequence in which the individual updates happen
guarantees that we end up with a consistent workspace. The set of projects
to update is described in the manifest.

Run "jiri help manifest" for details on manifests.

Usage:
  jiri update [flags] <file or url>

<file or url> points to snapshot to checkout.
`
}

func (c *updateCmd) Execute(ctx context.Context, f *flag.FlagSet, _ ...any) subcommands.ExitStatus {
	return executeWrapper(ctx, c.run, c.topLevelFlags, f.Args())
}

func (c *updateCmd) run(jirix *jiri.X, args []string) error {
	if len(args) > 1 {
		return jirix.UsageErrorf("unexpected number of arguments")
	}

	if c.attempts < 1 {
		return jirix.UsageErrorf("Number of attempts should be >= 1")
	}
	jirix.Attempts = c.attempts

	if c.autoupdate {
		// Try to update Jiri itself.
		if err := retry.Function(jirix, func() error {
			return jiri.UpdateAndExecute(jirix, c.forceAutoupdate)
		}, "download jiri binary", retry.AttemptsOpt(jirix.Attempts)); err != nil {
			fmt.Fprintf(jirix.Stdout(), "warning: automatic update failed: %v\n", err)
		}
	}
	if c.rebaseCurrent {
		jirix.Logger.Warningf("c. -rebase-current has been deprecated, please use -rebase-tracked.\n\n")
		c.rebaseTracked = true
	}

	if len(args) > 0 {
		jirix.OverrideOptional = c.overrideOptional
		if err := project.CheckoutSnapshot(jirix, args[0], c.gc, c.runHooks, c.fetchPkgs, c.hookTimeout, c.fetchPkgsTimeout, c.packagesToSkip); err != nil {
			return err
		}
	} else {
		lastSnapshot := jirix.UpdateHistoryLatestLink()
		duration := time.Duration(0)
		if info, err := os.Stat(lastSnapshot); err == nil {
			duration = time.Since(info.ModTime())
			if duration < minExecutionTimingThreshold || duration > maxExecutionTimingThreshold {
				duration = time.Duration(0)
			}
		}

		err := project.UpdateUniverse(jirix, project.UpdateUniverseParams{
			GC:                   c.gc,
			LocalManifest:        c.localManifest,
			RebaseTracked:        c.rebaseTracked,
			RebaseUntracked:      c.rebaseUntracked,
			RebaseAll:            c.rebaseAll,
			RunHooks:             c.runHooks,
			FetchPackages:        c.fetchPkgs,
			RebaseSubmodules:     c.rebaseSubmodules,
			RunHookTimeout:       c.hookTimeout,
			FetchPackagesTimeout: c.fetchPkgsTimeout,
			PackagesToSkip:       c.packagesToSkip,
		})
		if err2 := project.WriteUpdateHistorySnapshot(jirix, nil, nil, c.localManifest); err2 != nil {
			if err != nil {
				return fmt.Errorf("while updating: %s, while writing history: %s", err, err2)
			}
			return fmt.Errorf("while writing history: %s", err2)
		}
		if err != nil {
			return err
		}

		// Only track on successful update
		if duration.Nanoseconds() > 0 {
			jirix.AnalyticsSession.AddCommandExecutionTiming("update", duration)
		}
	}

	if jirix.Failures() != 0 {
		return fmt.Errorf("Project update completed with non-fatal errors")
	}

	if err := project.WriteUpdateHistoryLog(jirix); err != nil {
		jirix.Logger.Errorf("Failed to save jiri logs: %v", err)
	}
	return nil
}
