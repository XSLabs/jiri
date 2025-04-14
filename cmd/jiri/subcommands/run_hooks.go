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

type runHooksCmd struct {
	cmdBase

	localManifest         bool
	localManifestProjects arrayFlag
	hookTimeout           uint
	attempts              uint
	fetchPackages         bool
	packagesToSkip        arrayFlag
}

func (c *runHooksCmd) Name() string     { return "run-hooks" }
func (c *runHooksCmd) Synopsis() string { return "Run hooks using local manifest" }
func (c *runHooksCmd) Usage() string {
	return `Run hooks using local manifest JIRI_HEAD version if -local-manifest flag is
false, else it runs hooks using current manifest checkout version.

Usage:
  jiri run-hooks [flags]
`
}

func (c *runHooksCmd) SetFlags(f *flag.FlagSet) {
	f.BoolVar(&c.localManifest, "local-manifest", false, "Use local checked out manifest.")
	f.UintVar(&c.hookTimeout, "hook-timeout", project.DefaultHookTimeout, "Timeout in minutes for running the hooks operation.")
	f.UintVar(&c.attempts, "attempts", 1, "Number of attempts before failing.")
	f.BoolVar(&c.fetchPackages, "fetch-packages", true, "Use fetching packages using jiri.")
	f.Var(&c.packagesToSkip, "package-to-skip", "Skip fetching this package. Repeatable.")
	f.Var(&c.localManifestProjects, "local-manifest-project", "Import projects whose local manifests should be respected. Repeatable.")
}

func (c *runHooksCmd) Execute(ctx context.Context, f *flag.FlagSet, _ ...any) subcommands.ExitStatus {
	return executeWrapper(ctx, c.run, c.topLevelFlags, f.Args())
}

func (c *runHooksCmd) run(jirix *jiri.X, args []string) (err error) {
	localProjects, err := project.LocalProjects(jirix, project.FastScan)
	if err != nil {
		return err
	}
	if c.attempts < 1 {
		return jirix.UsageErrorf("Number of attempts should be >= 1")
	}
	jirix.Attempts = c.attempts

	// Get hooks.
	var hooks project.Hooks
	var pkgs project.Packages

	// Similar to project subcommand: do not use updated manifest if localManifestProjects is set. Only use updated manifest if the legacy
	// local manifest flag is set, to maintain legacy behavior.
	if !c.localManifest {
		_, hooks, pkgs, err = project.LoadUpdatedManifest(jirix, localProjects, c.localManifestProjects)
	} else {
		if len(c.localManifestProjects) == 0 {
			c.localManifestProjects, err = getDefaultLocalManifestProjects(jirix)
			if err != nil {
				return err
			}
		}
		_, hooks, pkgs, err = project.LoadManifestFile(jirix, jirix.JiriManifestFile(), localProjects, c.localManifestProjects)
	}
	if err != nil {
		return err
	}

	// If fetchPackages is true, fetch packages before running hooks in case
	// the hooks rely on the packages being available in the checkout.
	if err := project.FilterOptionalProjectsPackages(jirix, jirix.FetchingAttrs, nil, pkgs); err != nil {
		return err
	}
	project.FilterPackagesByName(jirix, pkgs, c.packagesToSkip)
	if c.fetchPackages && len(pkgs) > 0 {
		// Extend timeout for packages to be 5 times the timeout of a single hook.
		if err := project.FetchPackages(jirix, pkgs, c.hookTimeout*5); err != nil {
			return err
		}
	}

	return project.RunHooks(jirix, hooks, c.hookTimeout)
}
