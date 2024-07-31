// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package subcommands

import (
	"fmt"
	"os"
	"time"

	"go.fuchsia.dev/jiri"
	"go.fuchsia.dev/jiri/cmdline"
	"go.fuchsia.dev/jiri/project"
	"go.fuchsia.dev/jiri/retry"
)

var updateFlags struct {
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

const (
	MIN_EXECUTION_TIMING_THRESHOLD time.Duration = time.Duration(30) * time.Minute        // 30 min
	MAX_EXECUTION_TIMING_THRESHOLD time.Duration = time.Duration(2) * time.Hour * 24 * 14 // 2 weeks
)

func init() {
	cmdUpdate.Flags.BoolVar(&updateFlags.gc, "gc", true, "Garbage collect obsolete repositories.")
	cmdUpdate.Flags.BoolVar(&updateFlags.localManifest, "local-manifest", false, "Use local manifest")
	cmdUpdate.Flags.UintVar(&updateFlags.attempts, "attempts", 3, "Number of attempts before failing.")
	cmdUpdate.Flags.BoolVar(&updateFlags.autoupdate, "autoupdate", true, "Automatically update to the new version.")
	cmdUpdate.Flags.BoolVar(&updateFlags.forceAutoupdate, "force-autoupdate", false, "Always update to the current version.")
	cmdUpdate.Flags.BoolVar(&updateFlags.rebaseUntracked, "rebase-untracked", false, "Rebase untracked branches onto HEAD.")
	cmdUpdate.Flags.UintVar(&updateFlags.hookTimeout, "hook-timeout", project.DefaultHookTimeout, "Timeout in minutes for running the hooks operation.")
	cmdUpdate.Flags.UintVar(&updateFlags.fetchPkgsTimeout, "fetch-packages-timeout", project.DefaultPackageTimeout, "Timeout in minutes for fetching prebuilt packages using cipd.")
	cmdUpdate.Flags.BoolVar(&updateFlags.rebaseAll, "rebase-all", false, "Rebase all tracked branches. Also rebase all untracked branches if -rebase-untracked is passed")
	cmdUpdate.Flags.BoolVar(&updateFlags.rebaseCurrent, "rebase-current", false, "Deprecated. Implies -rebase-tracked. Would be removed in future.")
	cmdUpdate.Flags.BoolVar(&updateFlags.rebaseSubmodules, "rebase-submodules", false, "Rebase current tracked branches for submodules.")
	cmdUpdate.Flags.BoolVar(&updateFlags.rebaseTracked, "rebase-tracked", false, "Rebase current tracked branches instead of fast-forwarding them.")
	cmdUpdate.Flags.BoolVar(&updateFlags.runHooks, "run-hooks", true, "Run hooks after updating sources.")
	cmdUpdate.Flags.BoolVar(&updateFlags.fetchPkgs, "fetch-packages", true, "Use cipd to fetch packages.")
	cmdUpdate.Flags.BoolVar(&updateFlags.overrideOptional, "override-optional", false, "Override existing optional attributes in the snapshot file with current jiri settings")
	cmdUpdate.Flags.Var(&updateFlags.packagesToSkip, "package-to-skip", "Skip fetching this package. Repeatable.")
}

// cmdUpdate represents the "jiri update" command.
var cmdUpdate = &cmdline.Command{
	Runner: jiri.RunnerFunc(runUpdate),
	Name:   "update",
	Short:  "Update all jiri projects",
	Long: `
Updates all projects. The sequence in which the individual updates happen
guarantees that we end up with a consistent workspace. The set of projects
to update is described in the manifest.

Run "jiri help manifest" for details on manifests.
`,
	ArgsName: "<file or url>",
	ArgsLong: "<file or url> points to snapshot to checkout.",
}

func runUpdate(jirix *jiri.X, args []string) error {
	if len(args) > 1 {
		return jirix.UsageErrorf("unexpected number of arguments")
	}

	if updateFlags.attempts < 1 {
		return jirix.UsageErrorf("Number of attempts should be >= 1")
	}
	jirix.Attempts = updateFlags.attempts

	if updateFlags.autoupdate {
		// Try to update Jiri itself.
		if err := retry.Function(jirix, func() error {
			return jiri.UpdateAndExecute(updateFlags.forceAutoupdate)
		}, fmt.Sprintf("download jiri binary"), retry.AttemptsOpt(jirix.Attempts)); err != nil {
			fmt.Fprintf(jirix.Stdout(), "warning: automatic update failed: %v\n", err)
		}
	}
	if updateFlags.rebaseCurrent {
		jirix.Logger.Warningf("updateFlags. -rebase-current has been deprecated, please use -rebase-tracked.\n\n")
		updateFlags.rebaseTracked = true
	}

	if len(args) > 0 {
		jirix.OverrideOptional = updateFlags.overrideOptional
		if err := project.CheckoutSnapshot(jirix, args[0], updateFlags.gc, updateFlags.runHooks, updateFlags.fetchPkgs, updateFlags.hookTimeout, updateFlags.fetchPkgsTimeout, updateFlags.packagesToSkip); err != nil {
			return err
		}
	} else {
		lastSnapshot := jirix.UpdateHistoryLatestLink()
		duration := time.Duration(0)
		if info, err := os.Stat(lastSnapshot); err == nil {
			duration = time.Since(info.ModTime())
			if duration < MIN_EXECUTION_TIMING_THRESHOLD || duration > MAX_EXECUTION_TIMING_THRESHOLD {
				duration = time.Duration(0)
			}
		}

		err := project.UpdateUniverse(jirix, project.UpdateUniverseParams{
			GC:                   updateFlags.gc,
			LocalManifest:        updateFlags.localManifest,
			RebaseTracked:        updateFlags.rebaseTracked,
			RebaseUntracked:      updateFlags.rebaseUntracked,
			RebaseAll:            updateFlags.rebaseAll,
			RunHooks:             updateFlags.runHooks,
			FetchPackages:        updateFlags.fetchPkgs,
			RebaseSubmodules:     updateFlags.rebaseSubmodules,
			RunHookTimeout:       updateFlags.hookTimeout,
			FetchPackagesTimeout: updateFlags.fetchPkgsTimeout,
			PackagesToSkip:       updateFlags.packagesToSkip,
		})
		if err2 := project.WriteUpdateHistorySnapshot(jirix, nil, nil, updateFlags.localManifest); err2 != nil {
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
