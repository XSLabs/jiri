// Copyright 2018 The Fuchsia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package subcommands

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strings"
	"text/template"

	"github.com/google/subcommands"
	"go.fuchsia.dev/jiri"
	"go.fuchsia.dev/jiri/project"
)

// TODO(https://fxbug.dev/356134056): delete when finished migrating to
// subcommands library.
var (
	manifestFlags manifestCmd
	cmdManifest   = commandFromSubcommand(&manifestFlags)
)

// TODO(https://fxbug.dev/356134056): delete when finished migrating to
// subcommands library.
func init() {
	manifestFlags.SetFlags(&cmdManifest.Flags)
}

// manifestCmd defines the command-line flags for the manifest command.
type manifestCmd struct {
	// ElementName is a flag specifying the name= of the <import> or <project>
	// to search for in the manifest file.
	ElementName string

	// Template is a string template from pkg/text/template specifying which
	// fields to display.
	// The invoker of Jiri is expected to form this template
	// themselves.
	Template string
}

func (c *manifestCmd) Name() string { return "manifest" }
func (c *manifestCmd) Synopsis() string {
	return "Reads <import>, <project> or <package> information from a manifest file"
}
func (c *manifestCmd) Usage() string {
	return `
Reads <import>, <project> or <package> information from a manifest file.
A template matching the schema defined in pkg/text/template is used to fill
in the requested information.

Some examples:

Read project's 'remote' attribute:
manifest -element=$PROJECT_NAME -template="{{.Remote}}"

Read import's 'path' attribute:
manifest -element=$IMPORT_NAME -template="{{.Path}}"

Read packages's 'version' attribute:
manifest -element=$PACKAGE_NAME -template="{{.Version}}"

Usage:
  jiri manifest [flags] <manifest>

<manifest> is the manifest file.
`
}

func (c *manifestCmd) SetFlags(f *flag.FlagSet) {
	f.StringVar(&c.ElementName, "element", "", "Name of the <project>, <import> or <package>.")
	f.StringVar(&c.Template, "template", "", "The template for the fields to display.")
}

func (c *manifestCmd) Execute(ctx context.Context, _ *flag.FlagSet, args ...any) subcommands.ExitStatus {
	return executeWrapper(ctx, c.run, args)
}

func (c *manifestCmd) run(jirix *jiri.X, args []string) error {
	if len(args) != 1 {
		return jirix.UsageErrorf("Wrong number of args")
	}
	manifestPath := args[0]

	if c.ElementName == "" {
		return errors.New("-element is required")
	}
	if c.Template == "" {
		return errors.New("-template is required")
	}

	// Create the template to fill in.
	tmpl, err := template.New("").Parse(c.Template)
	if err != nil {
		return fmt.Errorf("failed to parse -template: %s", err)
	}

	return c.readManifest(jirix, manifestPath, tmpl)
}

func (c *manifestCmd) readManifest(jirix *jiri.X, manifestPath string, tmpl *template.Template) error {
	manifest, err := project.ManifestFromFile(jirix, manifestPath)
	if err != nil {
		return err
	}

	elementName := strings.ToLower(c.ElementName)

	// Check if any <project> elements match the given element name.
	for _, project := range manifest.Projects {
		if strings.ToLower(project.Name) == elementName {
			return tmpl.Execute(jirix.Stdout(), &project)
		}
	}

	// Check if any <import> elements match the given element name.
	for _, imprt := range manifest.Imports {
		if strings.ToLower(imprt.Name) == elementName {
			return tmpl.Execute(jirix.Stdout(), &imprt)
		}
	}

	// Check if any <package> elements match the given element name.
	for _, pkg := range manifest.Packages {
		if strings.ToLower(pkg.Name) == elementName {
			return tmpl.Execute(jirix.Stdout(), &pkg)
		}
	}

	// Found nothing.
	return fmt.Errorf("found no project/import/package named %s", manifestFlags.ElementName)
}
