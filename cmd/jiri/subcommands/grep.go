// Copyright 2016 The Fuchsia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package subcommands

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/google/subcommands"
	"go.fuchsia.dev/jiri"
	"go.fuchsia.dev/jiri/gitutil"
	"go.fuchsia.dev/jiri/project"
)

// TODO(https://fxbug.dev/356134056): delete when finished migrating to
// subcommands library.
var (
	grepFlags grepCmd
	cmdGrep   = commandFromSubcommand(&grepFlags)
)

// TODO(https://fxbug.dev/356134056): delete when finished migrating to
// subcommands library.
func init() {
	grepFlags.SetFlags(&cmdGrep.Flags)
}

type grepCmd struct {
	cmdBase

	cwdRel                   bool
	lineNumbers              bool
	h                        bool
	caseInsensitive          bool
	pattern                  string
	filenamesOnly            bool
	nonmatchingFilenamesOnly bool
	wordBoundaries           bool
}

func (c *grepCmd) Name() string     { return "grep" }
func (c *grepCmd) Synopsis() string { return "Search across projects." }
func (c *grepCmd) Usage() string {
	return `Run git grep across all projects.

Usage:
  jiri grep [flags] <query> [--] [<pathspec>...]
`
}

func (c *grepCmd) SetFlags(f *flag.FlagSet) {
	f.BoolVar(&c.lineNumbers, "n", false, "Prefix the line number to matching lines")
	f.StringVar(&c.pattern, "e", "", "The next parameter is the pattern. This option has to be used for patterns starting with -")
	f.BoolVar(&c.h, "H", true, "Does nothing. Just makes this git grep compatible")
	f.BoolVar(&c.caseInsensitive, "i", false, "Ignore case differences between the patterns and the files")
	f.BoolVar(&c.filenamesOnly, "l", false, "Instead of showing every matched line, show only the names of files that contain matches")
	f.BoolVar(&c.wordBoundaries, "w", false, "Match the pattern only at word boundary")
	f.BoolVar(&c.filenamesOnly, "name-only", false, "same as -l")
	f.BoolVar(&c.filenamesOnly, "files-with-matches", false, "same as -l")
	f.BoolVar(&c.nonmatchingFilenamesOnly, "L", false, "Instead of showing every matched line, show only the names of files that do not contain matches")
	f.BoolVar(&c.nonmatchingFilenamesOnly, "files-without-match", false, "same as -L")
	f.BoolVar(&c.cwdRel, "cwd-rel", false, "Output paths relative to the current working directory (if available)")
}

func (c *grepCmd) Execute(ctx context.Context, f *flag.FlagSet, _ ...any) subcommands.ExitStatus {
	return executeWrapper(ctx, c.run, c.topLevelFlags, f.Args())
}

func (c *grepCmd) buildFlags() []string {
	var args []string
	if c.lineNumbers {
		args = append(args, "-n")
	}
	if c.pattern != "" {
		args = append(args, "-e", c.pattern)
	}
	if c.caseInsensitive {
		args = append(args, "-i")
	}
	if c.filenamesOnly {
		args = append(args, "-l")
	}
	if c.nonmatchingFilenamesOnly {
		args = append(args, "-L")
	}
	if c.wordBoundaries {
		args = append(args, "-w")
	}
	return args
}

func (c *grepCmd) doGrep(jirix *jiri.X, args []string) ([]string, error) {
	var pathSpecs []string
	lenArgs := len(args)
	if lenArgs > 0 {
		for i, a := range os.Args {
			if a == "--" {
				pathSpecs = os.Args[i+1:]
				break
			}
		}
		// we will not find -- if user uses something like jiri grep -- a b,
		// as flag.Parse() removes '--' in that case, so set args length
		lenArgs = len(args) - len(pathSpecs)
		for i, a := range args {

			if a == "--" {
				args = args[0:i]
				// reset length
				lenArgs = len(args)
				break
			}
		}
	}

	if c.pattern != "" && lenArgs > 0 {
		return nil, jirix.UsageErrorf("No additional argument allowed with flag -e")
	} else if c.pattern == "" && lenArgs != 1 {
		return nil, jirix.UsageErrorf("grep requires one argument")
	}

	projects, err := project.LocalProjects(jirix, project.FastScan)
	if err != nil {
		return nil, err
	}

	// TODO(ianloic): run in parallel rather than serially.
	// TODO(ianloic): only run grep on projects under the cwd.
	var results []string
	flags := c.buildFlags()
	if jirix.Color.Enabled() {
		flags = append(flags, "--color=always")
	}
	query := ""
	if lenArgs == 1 {
		query = args[0]
	}

	cwd := jirix.Root
	if c.cwdRel {
		cwd = jirix.Cwd
	}

	for _, project := range projects {
		relpath, err := filepath.Rel(cwd, project.Path)
		if err != nil {
			return nil, err
		}
		git := gitutil.New(jirix, gitutil.RootDirOpt(project.Path))
		lines, err := git.Grep(query, pathSpecs, flags...)
		if err != nil {
			continue
		}
		for _, line := range lines {
			// TODO(ianloic): higlight the project path part like `repo grep`.
			results = append(results, relpath+"/"+line)
		}
	}

	// TODO(ianloic): fail if all of the sub-greps fail
	return results, nil
}

func (c *grepCmd) run(jirix *jiri.X, args []string) error {
	lines, err := c.doGrep(jirix, args)
	if err != nil {
		return err
	}

	for _, line := range lines {
		fmt.Fprintln(jirix.Stdout(), line)
	}
	return nil
}
