// Copyright 2018 The Fuchsia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package subcommands

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/google/subcommands"
	"go.fuchsia.dev/jiri"
	"go.fuchsia.dev/jiri/project"
)

// TODO(https://fxbug.dev/356134056): delete when finished migrating to
// subcommands library.
var (
	overrideFlags overrideCmd
	cmdOverride   = commandFromSubcommand(&overrideFlags)
)

// TODO(https://fxbug.dev/356134056): delete when finished migrating to
// subcommands library.
func init() {
	overrideFlags.SetFlags(&cmdOverride.Flags)
}

func (c *overrideCmd) SetFlags(f *flag.FlagSet) {
	f.StringVar(&c.importManifest, "import-manifest", "", "The manifest of the import override.")
	f.StringVar(&c.path, "path", "", `Path used to store the project locally.`)
	f.StringVar(&c.revision, "revision", "", `Revision to check out for the remote (defaults to HEAD).`)
	f.StringVar(&c.gerritHost, "gerrithost", "", `The project Gerrit host.`)
	f.BoolVar(&c.delete, "delete", false, `Delete existing override. Override is matched using <name> and <remote>, <remote> is optional.`)
	f.BoolVar(&c.list, "list", false, `List all the overrides from .jiri_manifest. This flag doesn't accept any arguments. -json-out flag can be used to specify json output file.`)
	f.StringVar(&c.jsonOutput, "json-output", "", `JSON output file from -list flag.`)
}

type overrideCmd struct {
	// Flags configuring project attributes for overrides.
	importManifest string
	gerritHost     string
	path           string
	revision       string
	// Flags controlling the behavior of the command.
	delete     bool
	list       bool
	jsonOutput string
}

func (c *overrideCmd) Name() string     { return "override" }
func (c *overrideCmd) Synopsis() string { return "Add overrides to .jiri_manifest file" }
func (c *overrideCmd) Usage() string {
	return `Add overrides to the .jiri_manifest file. This allows overriding project
definitions, including from transitively imported manifests.

Example:
 $ jiri override project https://foo.com/bar.git

Run "jiri help manifest" for details on manifests.

Usage:
  jiri override [flags] <name> <remote>

<name> is the project name.

<remote> is the project remote.
`
}

type overrideInfo struct {
	Import         bool   `json:"import,omitempty"`
	ImportManifest string `json:"import-manifest,omitempty"`
	Name           string `json:"name"`
	Path           string `json:"path,omitempty"`
	Remote         string `json:"remote"`
	Revision       string `json:"revision,omitempty"`
	GerritHost     string `json:"gerrithost,omitempty"`
}

func (c *overrideCmd) Execute(ctx context.Context, _ *flag.FlagSet, args ...any) subcommands.ExitStatus {
	return executeWrapper(ctx, c.run, args)
}

func (c *overrideCmd) run(jirix *jiri.X, args []string) error {
	if c.delete && c.list {
		return jirix.UsageErrorf("cannot use -delete and -list together")
	}

	if c.list && len(args) != 0 {
		return jirix.UsageErrorf("wrong number of arguments for the list flag")
	} else if c.delete && len(args) != 1 && len(args) != 2 {
		return jirix.UsageErrorf("wrong number of arguments for the delete flag")
	} else if !c.delete && !c.list && len(args) != 2 {
		return jirix.UsageErrorf("wrong number of arguments")
	}

	// Initialize manifest.
	manifestExists, err := isFile(jirix.JiriManifestFile())
	if err != nil {
		return err
	}
	if !manifestExists {
		return fmt.Errorf("'%s' does not exist", jirix.JiriManifestFile())
	}
	manifest, err := project.ManifestFromFile(jirix, jirix.JiriManifestFile())
	if err != nil {
		return err
	}

	if c.list {
		overrides := make([]overrideInfo, 0)
		for _, p := range manifest.ProjectOverrides {
			overrides = append(overrides, overrideInfo{
				Name:       p.Name,
				Path:       p.Path,
				Remote:     p.Remote,
				Revision:   p.Revision,
				GerritHost: p.GerritHost,
			})
		}

		for _, p := range manifest.ImportOverrides {
			overrides = append(overrides, overrideInfo{
				Import:         true,
				ImportManifest: p.Manifest,
				Name:           p.Name,
				Remote:         p.Remote,
				Revision:       p.Revision,
			})
		}

		if c.jsonOutput == "" {
			for _, o := range overrides {
				fmt.Fprintf(jirix.Stdout(), "* override %s\n", o.Name)
				if o.Import {
					fmt.Fprintf(jirix.Stdout(), "  IsImport: %v\n", o.Import)
					fmt.Fprintf(jirix.Stdout(), "  ImportManifest: %s\n", o.ImportManifest)
				}
				fmt.Fprintf(jirix.Stdout(), "  Name:        %s\n", o.Name)
				fmt.Fprintf(jirix.Stdout(), "  Remote:      %s\n", o.Remote)
				if o.Path != "" {
					fmt.Fprintf(jirix.Stdout(), "  Path:        %s\n", o.Path)
				}
				if o.Remote != "" {
					fmt.Fprintf(jirix.Stdout(), "  Revision:    %s\n", o.Revision)
				}
				if o.GerritHost != "" {
					fmt.Fprintf(jirix.Stdout(), "  Gerrit Host: %s\n", o.GerritHost)
				}
			}
		} else {
			file, err := os.Create(c.jsonOutput)
			if err != nil {
				return fmt.Errorf("failed to create output JSON file: %v\n", err)
			}
			defer file.Close()
			encoder := json.NewEncoder(file)
			encoder.SetIndent("", "  ")
			if err := encoder.Encode(overrides); err != nil {
				return fmt.Errorf("failed to serialize JSON output: %v\n", err)
			}
		}
		return nil
	}

	name := args[0]
	if c.delete {
		var projectOverrides []project.Project
		var importOverrides []project.Import
		var deletedProjectOverrides []project.Project
		var deletedImportOverrides []project.Import
		for _, p := range manifest.ImportOverrides {
			if c.importManifest == "" || (len(args) == 2 && p.Remote != args[1]) || p.Name != name {
				importOverrides = append(importOverrides, p)
				continue
			}
			deletedImportOverrides = append(deletedImportOverrides, p)
		}

		for _, p := range manifest.ProjectOverrides {
			if c.importManifest != "" || (len(args) == 2 && p.Remote != args[1]) || p.Name != name {
				projectOverrides = append(projectOverrides, p)
				continue
			}
			deletedProjectOverrides = append(deletedProjectOverrides, p)
		}

		if len(deletedProjectOverrides)+len(deletedImportOverrides) > 1 {
			return fmt.Errorf("more than one override matches")
		}
		var names []string
		for _, p := range deletedProjectOverrides {
			names = append(names, p.Name)
		}
		for _, p := range deletedImportOverrides {
			names = append(names, p.Name)
		}
		jirix.Logger.Infof("Deleted overrides: %s\n", strings.Join(names, " "))

		manifest.ProjectOverrides = projectOverrides
		manifest.ImportOverrides = importOverrides
	} else {
		remote := args[1]
		overrideKeys := make(map[string]bool)
		for _, p := range manifest.ProjectOverrides {
			overrideKeys[p.Key().String()] = true
		}
		for _, p := range manifest.ImportOverrides {
			overrideKeys[p.ProjectKey().String()] = true
		}
		if _, ok := overrideKeys[project.MakeProjectKey(name, remote).String()]; !ok {
			if c.importManifest != "" {
				importOverride := project.Import{
					Name:     name,
					Remote:   remote,
					Manifest: c.importManifest,
					Revision: c.revision,
				}
				manifest.ImportOverrides = append(manifest.ImportOverrides, importOverride)
			} else {
				projectOverride := project.Project{
					Name:       name,
					Remote:     remote,
					Path:       c.path,
					Revision:   c.revision,
					GerritHost: c.gerritHost,
					// We deliberately omit RemoteBranch, HistoryDepth and
					// GitHooks. Those fields are effectively deprecated and
					// will likely be removed in the future.
				}
				manifest.ProjectOverrides = append(manifest.ProjectOverrides, projectOverride)
			}
		} else {
			jirix.Logger.Infof("Override \"%s:%s\" is already exist, no modification will be made.", name, remote)
		}
	}

	// There's no error checking when writing the .jiri_manifest file;
	// errors will be reported when "jiri update" is run.
	return manifest.ToFile(jirix, jirix.JiriManifestFile())
}
