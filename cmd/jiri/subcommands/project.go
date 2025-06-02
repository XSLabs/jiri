// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package subcommands

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"text/template"

	"github.com/google/subcommands"
	"go.fuchsia.dev/jiri"
	"go.fuchsia.dev/jiri/project"
)

type projectCmd struct {
	cmdBase

	cleanAll              bool
	cleanup               bool
	jsonOutput            string
	regexp                bool
	template              string
	useLocalManifest      bool
	useRemoteProjects     bool
	localManifestProjects arrayFlag
}

func (c *projectCmd) Name() string     { return "project" }
func (c *projectCmd) Synopsis() string { return "Manage the jiri projects" }
func (c *projectCmd) Usage() string {
	return `Cleans all projects if -clean flag is provided else inspect
the local filesystem and provide structured info on the existing
projects and branches. Projects are specified using either names or
regular expressions that are matched against project names. If no
command line arguments are provided the project that the contains the
current directory is used, or if run from outside of a given project,
all projects will be used. The information to be displayed can be
specified using a Go template, supplied via
the -template flag.

Usage:
  jiri project [flags] <project ...>

<project ...> is a list of projects to clean up or give info about.
`
}

func (c *projectCmd) SetFlags(f *flag.FlagSet) {
	f.BoolVar(&c.cleanAll, "clean-all", false, "Restore jiri projects to their pristine state and delete all branches.")
	f.BoolVar(&c.cleanup, "clean", false, "Restore jiri projects to their pristine state.")
	f.StringVar(&c.jsonOutput, "json-output", "", "Path to write operation results to.")
	f.BoolVar(&c.regexp, "regexp", false, "Use argument as regular expression.")
	f.StringVar(&c.template, "template", "", "The template for the fields to display.")
	f.BoolVar(&c.useLocalManifest, "local-manifest", false, "List project status based on local manifest.")
	f.Var(&c.localManifestProjects, "local-manifest-project", "Import projects whose local manifests should be respected. Repeatable.")
	f.BoolVar(&c.useRemoteProjects, "list-remote-projects", false, "List remote projects instead of local projects.")
}

func (c *projectCmd) Execute(ctx context.Context, f *flag.FlagSet, _ ...any) subcommands.ExitStatus {
	return executeWrapper(ctx, c.run, c.topLevelFlags, f.Args())
}

func (c *projectCmd) run(jirix *jiri.X, args []string) (e error) {
	if c.cleanup || c.cleanAll {
		return c.runProjectClean(jirix, args)
	} else {
		return c.runProjectInfo(jirix, args)
	}
}

func (c *projectCmd) runProjectClean(jirix *jiri.X, args []string) (e error) {
	localProjects, err := project.LocalProjects(jirix, project.FullScan)
	if err != nil {
		return err
	}
	projects := make(project.Projects)
	if len(args) > 0 {
		if c.regexp {
			for _, a := range args {
				re, err := regexp.Compile(a)
				if err != nil {
					return fmt.Errorf("failed to compile regexp %v: %v", a, err)
				}
				for _, p := range localProjects {
					if re.MatchString(p.Name) {
						projects[p.Key()] = p
					}
				}
			}
		} else {
			for _, arg := range args {
				p, err := localProjects.FindUnique(arg)
				if err != nil {
					fmt.Fprintf(jirix.Stderr(), "Error finding local project %q: %v.\n", p.Name, err)
				} else {
					projects[p.Key()] = p
				}
			}
		}
	} else {
		projects = localProjects
	}
	if err := project.CleanupProjects(jirix, projects, c.cleanAll); err != nil {
		return err
	}
	return nil
}

// projectInfoOutput defines JSON format for 'project info' output.
type projectInfoOutput struct {
	Name string `json:"name"`
	Path string `json:"path"`

	// Relative path w.r.t to root
	RelativePath   string   `json:"relativePath"`
	Remote         string   `json:"remote"`
	Revision       string   `json:"revision"`
	CurrentBranch  string   `json:"current_branch,omitempty"`
	Branches       []string `json:"branches,omitempty"`
	Manifest       string   `json:"manifest,omitempty"`
	GerritHost     string   `json:"gerrithost,omitempty"`
	GitSubmoduleOf string   `json:"gitsubmoduleof,omitempty"`
}

// runProjectInfo provides structured info on local projects.
func (c *projectCmd) runProjectInfo(jirix *jiri.X, args []string) error {
	var tmpl *template.Template
	var err error
	if c.template != "" {
		tmpl, err = template.New("info").Parse(c.template)
		if err != nil {
			return fmt.Errorf("failed to parse template %q: %v", c.template, err)
		}
	}

	regexps := []*regexp.Regexp{}
	if len(args) > 0 && c.regexp {
		regexps = make([]*regexp.Regexp, len(args))
		for i, a := range args {
			re, err := regexp.Compile(a)
			if err != nil {
				return fmt.Errorf("failed to compile regexp %v: %v", a, err)
			}
			regexps[i] = re
		}
	}

	var states map[project.ProjectKey]*project.ProjectState
	var keys project.ProjectKeys
	projects, err := project.LocalProjects(jirix, project.FastScan)
	if err != nil {
		return err
	}

	// Similar to run-hooks logic: do not use updated manifest if localManifestProjects
	// is set. Only use updated manifest if the legacy local manifest flag is set, to
	// maintain legacy behavior.
	if c.useLocalManifest || len(c.localManifestProjects) != 0 {
		if len(c.localManifestProjects) == 0 {
			c.localManifestProjects, err = getDefaultLocalManifestProjects(jirix)
			if err != nil {
				return err
			}
		}
		projects, _, _, err = project.LoadUpdatedManifest(jirix, projects, c.localManifestProjects)
		if err := project.FilterOptionalProjectsPackages(jirix, jirix.FetchingAttrs, projects, nil); err != nil {
			return err
		}
	}

	if c.useRemoteProjects {
		projects, _, _, err = project.LoadManifestFile(jirix, jirix.JiriManifestFile(), projects, nil)
		if err != nil {
			return err
		}
		// filter optional projects
		// we only need to filter in the remote projects path, otherwise it is only using
		// projects that already exist on disk.
		if err := project.FilterOptionalProjectsPackages(jirix, jirix.FetchingAttrs, projects, nil); err != nil {
			return err
		}
	}
	if len(args) == 0 {
		currentProject, err := project.CurrentProject(jirix)
		if err != nil {
			return err
		}
		// Due to fuchsia.git is checked out at root.
		// set currentProject to nil if current working
		// dir is JIRI_ROOT to allow list all projects.
		cwd := jirix.Cwd
		if cwd == jirix.Root {
			currentProject = nil
		}
		if currentProject == nil {
			// jiri was run from outside of a project so let's
			// use all available projects.
			states, err = project.GetProjectStates(jirix, projects, false)
			if err != nil {
				return err
			}
			for key := range states {
				keys = append(keys, key)
			}
		} else {
			state, err := project.GetProjectState(jirix, *currentProject, true)
			if err != nil {
				return err
			}
			states = map[project.ProjectKey]*project.ProjectState{
				currentProject.Key(): state,
			}
			keys = append(keys, currentProject.Key())
		}
	} else {
		var err error
		states, err = project.GetProjectStates(jirix, projects, false)
		if err != nil {
			return err
		}
		for key, state := range states {
			if c.regexp {
				for _, re := range regexps {
					if re.MatchString(state.Project.Name) {
						keys = append(keys, key)
						break
					}
				}
			} else {
				for _, arg := range args {
					if arg == state.Project.Name {
						keys = append(keys, key)
						break
					}
				}
			}
		}
	}
	sort.Sort(keys)

	info := make([]projectInfoOutput, len(keys))
	for i, key := range keys {
		state := states[key]
		rp, err := filepath.Rel(jirix.Root, state.Project.Path)
		if err != nil {
			// should not happen
			panic(err)
		}
		info[i] = projectInfoOutput{
			Name:           state.Project.Name,
			Path:           state.Project.Path,
			RelativePath:   rp,
			Remote:         state.Project.Remote,
			Revision:       state.Project.Revision,
			CurrentBranch:  state.CurrentBranch.Name,
			Manifest:       state.Project.ManifestPath,
			GerritHost:     state.Project.GerritHost,
			GitSubmoduleOf: state.Project.GitSubmoduleOf,
		}
		for _, b := range state.Branches {
			info[i].Branches = append(info[i].Branches, b.Name)
		}
	}

	for _, i := range info {
		if c.template != "" {
			out := &bytes.Buffer{}
			if err := tmpl.Execute(out, i); err != nil {
				return jirix.UsageErrorf("invalid format")
			}
			fmt.Fprintln(jirix.Stdout(), out.String())
		} else {
			fmt.Fprintf(jirix.Stdout(), "* project %s\n", i.Name)
			fmt.Fprintf(jirix.Stdout(), "  Path:     %s\n", i.Path)
			fmt.Fprintf(jirix.Stdout(), "  Remote:   %s\n", i.Remote)
			fmt.Fprintf(jirix.Stdout(), "  Revision: %s\n", i.Revision)
			if i.GitSubmoduleOf != "" {
				fmt.Fprintf(jirix.Stdout(), "  GitSubmoduleOf: %s\n", i.GitSubmoduleOf)
			}
			if c.useRemoteProjects {
				fmt.Fprintf(jirix.Stdout(), "  Manifest: %s\n", i.Manifest)
			}
			if len(i.Branches) != 0 {
				fmt.Fprintf(jirix.Stdout(), "  Branches:\n")
				width := 0
				for _, b := range i.Branches {
					if len(b) > width {
						width = len(b)
					}
				}
				for _, b := range i.Branches {
					fmt.Fprintf(jirix.Stdout(), "    %-*s", width, b)
					if i.CurrentBranch == b {
						fmt.Fprintf(jirix.Stdout(), " current")
					}
					fmt.Fprintln(jirix.Stdout())
				}
			} else {
				fmt.Fprintf(jirix.Stdout(), "  Branches: none\n")
			}
		}
	}

	if c.jsonOutput != "" {
		if err := writeJSONOutput(c.jsonOutput, info); err != nil {
			return err
		}
	}

	return nil
}

func writeJSONOutput(path string, result any) error {
	out, err := json.MarshalIndent(&result, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize JSON output: %s", err)
	}

	err = os.WriteFile(path, out, 0600)
	if err != nil {
		return fmt.Errorf("failed write JSON output to %s: %s", path, err)
	}

	return nil
}
