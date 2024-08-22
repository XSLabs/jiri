// Copyright 2019 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package subcommands

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"text/template"

	"github.com/google/subcommands"
	"go.fuchsia.dev/jiri"
	"go.fuchsia.dev/jiri/cipd"
	"go.fuchsia.dev/jiri/project"
)

// TODO(https://fxbug.dev/356134056): delete when finished migrating to
// subcommands library.
var (
	packageFlags packageCmd
	cmdPackage   = commandFromSubcommand(&packageFlags)
)

// TODO(https://fxbug.dev/356134056): delete when finished migrating to
// subcommands library.
func init() {
	packageFlags.SetFlags(&cmdPackage.Flags)
}

type packageCmd struct {
	cmdBase

	jsonOutput string
	regexp     bool
}

func (c *packageCmd) Name() string     { return "package" }
func (c *packageCmd) Synopsis() string { return "Display the jiri packages" }
func (c *packageCmd) Usage() string {
	return `
Display structured info on the existing
packages and branches. Packages are specified using either names or	regular
expressions that are matched against package names. If no command line
arguments are provided all projects will be used.

Usage:
  jiri package [flags] <package ...>

<package ...> is a list of packages to give info about.
`
}

func (c *packageCmd) SetFlags(f *flag.FlagSet) {
	f.StringVar(&c.jsonOutput, "json-output", "", "Path to write operation results to.")
	f.BoolVar(&c.regexp, "regexp", false, "Use argument as regular expression.")
}

func (c *packageCmd) Execute(ctx context.Context, f *flag.FlagSet, _ ...any) subcommands.ExitStatus {
	return executeWrapper(ctx, c.run, c.topLevelFlags, f.Args())
}

// runPackageInfo provides structured info on packages.
func (c *packageCmd) run(jirix *jiri.X, args []string) error {
	var err error

	regexps := make([]*regexp.Regexp, 0)
	for _, arg := range args {
		if !c.regexp {
			arg = "^" + regexp.QuoteMeta(arg) + "$"
		}
		if re, err := regexp.Compile(arg); err != nil {
			return fmt.Errorf("failed to compile regexp %v: %v", arg, err)
		} else {
			regexps = append(regexps, re)
		}
	}

	projects, err := project.LocalProjects(jirix, project.FastScan)
	if err != nil {
		return err
	}
	_, _, pkgs, err := project.LoadManifestFile(jirix, jirix.JiriManifestFile(), projects, true)
	if err != nil {
		return err
	}
	var keys project.PackageKeys
	for k, v := range pkgs {
		if len(args) == 0 {
			keys = append(keys, k)
		} else {
			for _, re := range regexps {
				if re.MatchString(v.Name) {
					keys = append(keys, k)
					break
				}
			}
		}
	}

	sort.Sort(keys)

	info := make([]packageInfoOutput, 0)
	for _, key := range keys {
		pkg := pkgs[key]
		pkgPath, err := pkg.GetPath()
		if err != nil {
			return err
		}
		tmpl, err := template.New("pack").Parse(pkgPath)
		if err != nil {
			return fmt.Errorf("parsing package path %q failed", pkgPath)
		}
		var subdirBuf bytes.Buffer
		// subdir is using fuchsia platform format instead of
		// using cipd platform format
		tmpl.Execute(&subdirBuf, cipd.FuchsiaPlatform(cipd.CipdPlatform))
		pkgPath = filepath.Join(jirix.Root, subdirBuf.String())

		platforms, err := pkg.GetPlatforms()
		if err != nil {
			return fmt.Errorf("parsing %s platforms failed", pkg.Name)
		}

		resolvedPlatforms := make([]string, 0, len(platforms))
		for _, p := range platforms {
			resolvedPlatforms = append(resolvedPlatforms, p.String())
		}

		info = append(info, packageInfoOutput{
			Name:      pkg.Name,
			Path:      pkgPath,
			Version:   pkg.Version,
			Manifest:  pkg.ManifestPath,
			Platforms: resolvedPlatforms,
		})
	}

	for _, i := range info {
		fmt.Fprintf(jirix.Stdout(), "* package %s\n", i.Name)
		fmt.Fprintf(jirix.Stdout(), "  Path:     %s\n", i.Path)
		fmt.Fprintf(jirix.Stdout(), "  Version:  %s\n", i.Version)
		fmt.Fprintf(jirix.Stdout(), "  Manifest: %s\n", i.Manifest)
		fmt.Fprintf(jirix.Stdout(), "  Platforms: %v\n", i.Platforms)
	}

	if c.jsonOutput != "" {
		if err := writeJSONOutput(c.jsonOutput, info); err != nil {
			return err
		}
	}

	return nil
}

// packageInfoOutput defines JSON format for 'project info' output.
type packageInfoOutput struct {
	Name      string   `json:"name"`
	Path      string   `json:"path"`
	Version   string   `json:"version"`
	Manifest  string   `json:"manifest,omitempty"`
	Platforms []string `json:"platforms,omitempty"`
}
