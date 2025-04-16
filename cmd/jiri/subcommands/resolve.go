// Copyright 2018 The Fuchsia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package subcommands

import (
	"context"
	"flag"
	"strings"

	"github.com/google/subcommands"
	"go.fuchsia.dev/jiri"
	"go.fuchsia.dev/jiri/project"
)

type resolveCmd struct {
	cmdBase

	lockFilePath          string
	localManifestFlag     bool
	enablePackageLock     bool
	enableProjectLock     bool
	enablePackageVersion  bool
	allowFloatingRefs     bool
	fullResolve           bool
	hostnameAllowList     string
	localManifestProjects arrayFlag
}

func (c *resolveCmd) AllowFloatingRefs() bool {
	return c.allowFloatingRefs
}

func (c *resolveCmd) LockFilePath() string {
	return c.lockFilePath
}

func (c *resolveCmd) LocalManifest() bool {
	return c.localManifestFlag
}

func (c *resolveCmd) LocalManifestProjects() []string {
	return c.localManifestProjects
}

func (c *resolveCmd) EnablePackageLock() bool {
	return c.enablePackageLock
}

func (c *resolveCmd) EnableProjectLock() bool {
	return c.enableProjectLock
}

func (c *resolveCmd) HostnameAllowList() []string {
	ret := make([]string, 0)
	hosts := strings.Split(c.hostnameAllowList, ",")
	for _, item := range hosts {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		ret = append(ret, item)
	}
	return ret
}

func (c *resolveCmd) FullResolve() bool {
	return c.fullResolve
}

func (c *resolveCmd) Name() string     { return "resolve" }
func (c *resolveCmd) Synopsis() string { return "Generate jiri lockfile" }
func (c *resolveCmd) Usage() string {
	return `Generate jiri lockfile in json format for <manifest ...>. If no manifest
provided, jiri will use .jiri_manifest by default.

Usage:
  jiri resolve [flags] <manifest ...>

<manifest ...> is a list of manifest files for lockfile generation
`
}

func (c *resolveCmd) SetFlags(f *flag.FlagSet) {
	f.StringVar(&c.lockFilePath, "output", "jiri.lock", "Path to the generated lockfile")
	f.BoolVar(&c.localManifestFlag, "local-manifest", false, "Use local manifest")
	f.BoolVar(&c.enablePackageLock, "enable-package-lock", true, "Enable resolving packages in lockfile")
	f.BoolVar(&c.enableProjectLock, "enable-project-lock", false, "Enable resolving projects in lockfile")
	f.BoolVar(&c.allowFloatingRefs, "allow-floating-refs", false, "Allow packages to be pinned to floating refs such as \"latest\"")
	f.StringVar(&c.hostnameAllowList, "allow-hosts", "", "List of hostnames that can be used in the url of a repository, separated by comma. It will not be enforced if it is left empty.")
	f.BoolVar(&c.fullResolve, "full-resolve", false, "Resolve all project and packages, not just those are changed.")
	f.Var(&c.localManifestProjects, "local-manifest-project", "Import projects whose local manifests should be respected. Repeatable.")
}

func (c *resolveCmd) Execute(ctx context.Context, f *flag.FlagSet, _ ...any) subcommands.ExitStatus {
	return executeWrapper(ctx, c.run, c.topLevelFlags, f.Args())
}

func (c *resolveCmd) run(jirix *jiri.X, args []string) error {
	manifestFiles := make([]string, 0)
	if len(args) == 0 {
		// Use .jiri_manifest if no manifest file path is present
		manifestFiles = append(manifestFiles, jirix.JiriManifestFile())
	} else {
		manifestFiles = append(manifestFiles, args...)
	}
	if c.localManifestFlag && len(c.localManifestProjects) == 0 {
		c.localManifestProjects, _ = getDefaultLocalManifestProjects(jirix)
	} else if !c.localManifestFlag {
		c.localManifestProjects = nil
	}

	// While revision pins for projects can be updated by 'jiri edit',
	// instance IDs of packages can only be updated by 'jiri resolve' due
	// to the way how cipd works. Since roller is using 'jiri resolve'
	// to update a single jiri.lock file each time, it will cause conflicting
	// instance ids between updated 'jiri.lock' and un-updated 'jiri.lock' files.
	// Jiri will halt when detecting conflicts in locks. So to make it work,
	// we need to temporarily disable the conflicts detection.
	jirix.IgnoreLockConflicts = true
	return project.GenerateJiriLockFile(jirix, manifestFiles, c)
}
