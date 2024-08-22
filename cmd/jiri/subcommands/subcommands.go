// Copyright 2024 The Fuchsia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package subcommands

import (
	"context"
	"errors"
	"flag"
	"fmt"

	"github.com/google/subcommands"
	"go.fuchsia.dev/jiri"
	"go.fuchsia.dev/jiri/cmdline"
)

type cmdBase struct {
	topLevelFlags jiri.TopLevelFlags
}

func NewCommander(args []string) (*subcommands.Commander, error) {
	f := flag.NewFlagSet("jiri", flag.ExitOnError)

	var flags jiri.TopLevelFlags
	flags.SetFlags(f)

	err := f.Parse(args)
	if err != nil {
		return nil, err
	}

	cdr := subcommands.NewCommander(f, "jiri")

	// Mark all top-level flags as important so they should up in the default
	// help text.
	f.VisitAll(func(flg *flag.Flag) {
		cdr.ImportantFlag(flg.Name)
	})

	b := cmdBase{topLevelFlags: flags}

	lowLevelGroup := "low-level operations"

	cdr.Register(cdr.HelpCommand(), "")
	cdr.Register(cdr.FlagsCommand(), "")
	cdr.Register(&branchCmd{cmdBase: b}, "")
	cdr.Register(&diffCmd{cmdBase: b}, "")
	cdr.Register(&grepCmd{cmdBase: b}, "")
	cdr.Register(&initCmd{cmdBase: b}, "")
	cdr.Register(&patchCmd{cmdBase: b}, "")
	cdr.Register(&runpCmd{cmdBase: b}, "")
	cdr.Register(&selfUpdateCmd{cmdBase: b}, "")
	cdr.Register(&statusCmd{cmdBase: b}, "")
	cdr.Register(&updateCmd{cmdBase: b}, "")
	cdr.Register(&uploadCmd{cmdBase: b}, "")
	cdr.Register(&versionCmd{cmdBase: b}, "")

	cdr.Register(&bootstrapCmd{cmdBase: b}, lowLevelGroup)
	cdr.Register(&checkCleanCmd{cmdBase: b}, lowLevelGroup)
	cdr.Register(&editCmd{cmdBase: b}, lowLevelGroup)
	cdr.Register(&fetchPkgsCmd{cmdBase: b}, lowLevelGroup)
	cdr.Register(&genGitModuleCmd{cmdBase: b}, lowLevelGroup)
	cdr.Register(&importCmd{cmdBase: b}, lowLevelGroup)
	cdr.Register(&manifestCmd{cmdBase: b}, lowLevelGroup)
	cdr.Register(&overrideCmd{cmdBase: b}, lowLevelGroup)
	cdr.Register(&packageCmd{cmdBase: b}, lowLevelGroup)
	cdr.Register(&projectCmd{cmdBase: b}, lowLevelGroup)
	cdr.Register(&projectConfigCmd{cmdBase: b}, lowLevelGroup)
	cdr.Register(&resolveCmd{cmdBase: b}, lowLevelGroup)
	cdr.Register(&runHooksCmd{cmdBase: b}, lowLevelGroup)
	cdr.Register(&snapshotCmd{cmdBase: b}, lowLevelGroup)
	cdr.Register(&sourceManifestCmd{cmdBase: b}, lowLevelGroup)

	return cdr, nil
}

type jiriSubcommand interface {
	subcommands.Command

	run(jirix *jiri.X, args []string) error
}

func commandFromSubcommand(s jiriSubcommand) *cmdline.Command {
	// Command represents a single command in a command-line program.  A program
	// with subcommands is represented as a root Command with children representing
	// each subcommand.  The command graph must be a tree; each command may either
	// have no parent (the root) or exactly one parent, and cycles are not allowed.
	return &cmdline.Command{
		Name:  s.Name(),
		Short: s.Synopsis(),
		Long:  s.Usage(),
	}
}

// executeWrapper converts a Jiri-style subcommand implementation into a
// subcommands.Command.Execute() function.
//
// Example:
//
//	func (c *fooCmd) Execute(ctx context.Context, f *flag.FlagSet, _ ...any) subcommands.ExitStatus {
//		executeWrapper(ctx, c.run, c.topLevelFlags, f.Args())
//	}
//	func (c *fooCmd) run(jirix *jiri.X, args []string) error {
//		// ... actual implementation of the command
//	}
func executeWrapper(ctx context.Context, f func(jirix *jiri.X, args []string) error, topLevelFlags jiri.TopLevelFlags, args []string) subcommands.ExitStatus {
	err := func() error {
		jirix, err := jiri.NewXFromContext(ctx, topLevelFlags)
		if err != nil {
			return err
		}
		defer jirix.RunCleanup()
		return f(jirix, args)
	}()
	return errToExitStatus(ctx, err)
}

func errToExitStatus(ctx context.Context, err error) subcommands.ExitStatus {
	if err != nil {
		env := cmdline.EnvFromContext(ctx)
		if env.Stderr != nil {
			fmt.Fprintf(env.Stderr, "ERROR: %s\n", err)
		}
		var exitCodeErr *cmdline.ErrExitCode
		if errors.As(err, &exitCodeErr) {
			return subcommands.ExitStatus(int(*exitCodeErr))
		}
		return subcommands.ExitFailure
	}
	return subcommands.ExitSuccess
}

var topicFileSystem = cmdline.Topic{
	Name:  "filesystem",
	Short: "Description of jiri file system layout",
	Long: `
All data managed by the jiri tool is located in the file system under a root
directory, colloquially called the jiri root directory.  The file system layout
looks like this:

 [root]                                   # root directory (name picked by user)
 [root]/.jiri_root                        # root metadata directory
 [root]/.jiri_root/bin                    # contains jiri tool binary
 [root]/.jiri_root/update_history         # contains history of update snapshots
 [root]/.manifest                         # contains jiri manifests
 [root]/[project1]                        # project directory (name picked by user)
 [root]/[project1]/.git/jiri              # project metadata directory
 [root]/[project1]/.git/jiri/metadata.v2  # project metadata file
 [root]/[project1]/.git/jiri/config       # project local config file
 [root]/[project1]/<<files>>              # project files
 [root]/[project2]...

The [root] and [projectN] directory names are picked by the user.  The <<cls>>
are named via jiri cl new, and the <<files>> are named as the user adds files
and directories to their project.  All other names above have special meaning to
the jiri tool, and cannot be changed; you must ensure your path names don't
collide with these special names.

To find the [root] directory, the jiri binary looks for the .jiri_root
directory, starting in the current working directory and walking up the
directory chain.  The search is terminated successfully when the .jiri_root
directory is found; it fails after it reaches the root of the file system.
Thus jiri must be invoked from the [root] directory or one of its
subdirectories.  To invoke jiri from a different directory, you can set the
-root flag to point to your [root] directory.

Keep in mind that when "jiri update" is run, the jiri tool itself is
automatically updated along with all projects.  Note that if you have multiple
[root] directories on your file system, you must remember to run the jiri
binary corresponding to your [root] directory.  Things may fail if you mix
things up, since the jiri binary is updated with each call to "jiri update",
and you may encounter version mismatches between the jiri binary and the
various metadata files or other logic.

The jiri binary is located at [root]/.jiri_root/bin/jiri
`,
}

var topicManifest = cmdline.Topic{
	Name:  "manifest-files",
	Short: "Description of manifest files",
	Long: `
Jiri manifest files describe the set of projects that get synced when running
"jiri update".

The first manifest file that jiri reads is in [root]/.jiri_manifest.  This
manifest **must** exist for the jiri tool to work.

Usually the manifest in [root]/.jiri_manifest will import other manifests from
remote repositories via <import> tags, but it can contain its own list of
projects as well.

Manifests have the following XML schema:

<manifest>
  <imports>
    <import remote="https://vanadium.googlesource.com/manifest"
            manifest="public"
            name="manifest"
    />
    <localimport file="/path/to/local/manifest"/>
    ...
  </imports>
  <projects>
    <project name="my-project"
             path="path/where/project/lives"
             protocol="git"
             remote="https://github.com/myorg/foo"
             revision="ed42c05d8688ab23"
             remotebranch="my-branch"
             gerrithost="https://myorg-review.googlesource.com"
             githooks="path/to/githooks-dir"
             attributes="attr1,attr2..."
    />
    ...
  </projects>
  <hooks>
    <hook name="update"
          project="mojo/public"
          action="update.sh"/>
    ...
  </hooks>

</manifest>

The <import> and <localimport> tags can be used to share common projects across
multiple manifests.

A <localimport> tag should be used when the manifest being imported and the
importing manifest are both in the same repository, or when neither one is in a
repository.  The "file" attribute is the path to the manifest file being
imported.  It can be absolute, or relative to the importing manifest file.

If the manifest being imported and the importing manifest are in different
repositories then an <import> tag must be used, with the following attributes:

* remote (required) - The remote url of the repository containing the
manifest to be imported

* manifest (required) - The path of the manifest file to be imported,
relative to the repository root.

* name (optional) - The name of the project corresponding to the manifest
repository.  If your manifest contains a <project> with the same remote as
the manifest remote, then the "name" attribute of on the <import> tag should
match the "name" attribute on the <project>.  Otherwise, jiri will clone the
manifest repository on every update.

The <project> tags describe the projects to sync, and what state they should
sync to, according to the following attributes:

* name (required) - The name of the project.

* path (required) - The location where the project will be located, relative to
the jiri root.

* remote (required) - The remote url of the project repository.

* protocol (optional) - The protocol to use when cloning and syncing the repo.
Currently "git" is the default and only supported protocol.

* remotebranch (optional) - The remote branch that the project will sync to.
Defaults to "main".  The "remotebranch" attribute is ignored if "revision"
is specified.

* revision (optional) - The specific revision (usually a git SHA) that the
project will sync to.  If "revision" is  specified then the "remotebranch"
attribute is ignored.

* gerrithost (optional) - The url of the Gerrit host for the project.  If
specified, then running "jiri cl upload" will upload a CL to this Gerrit host.

* githooks (optional) - The path (relative to [root]) of a directory containing
git hooks that will be installed in the projects .git/hooks directory during
each update.

* attributes (optional) - If this field is specified, the default behavior does
not fetch the project. However, you can include it by specifying the attribute
with the -fetch-optional flag in the 'jiri init' invocation.

The <hook> tag describes the hooks that must be executed after every 'jiri update'
They are configured via the following attributes:

* name (required) - The name of the of the hook to identify it

* project (required) - The name of the project where the hook is present

* action (required) - Action to be performed inside the project.
It is mostly identified by a script
`,
}
