// Copyright 2017 The Fuchsia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package subcommands

import (
	"context"
	"flag"
	"fmt"
	"strconv"

	"github.com/google/subcommands"
	"go.fuchsia.dev/jiri"
	"go.fuchsia.dev/jiri/project"
)

type projectConfigCmd struct {
	cmdBase

	ignore   string
	noUpdate string
	noRebase string
}

func (c *projectConfigCmd) Name() string     { return "project-config" }
func (c *projectConfigCmd) Synopsis() string { return "Prints/sets project's local config" }
func (c *projectConfigCmd) Usage() string {
	return `Prints/Manages local project config. This command should be run from inside a
project. It will print config if no flags are provided otherwise set it.

Usage:
  jiri project-config [flags]
`
}

func (c *projectConfigCmd) SetFlags(f *flag.FlagSet) {
	f.StringVar(&c.ignore, "ignore", "", `This can be true or false. If set to true project would be completely ignored while updating`)
	f.StringVar(&c.noUpdate, "no-update", "", `This can be true or false. If set to true project won't be updated`)
	f.StringVar(&c.noRebase, "no-rebase", "", `This can be true or false. If set to true local branch won't be rebased or merged.`)
}

func (c *projectConfigCmd) Execute(ctx context.Context, f *flag.FlagSet, _ ...any) subcommands.ExitStatus {
	return executeWrapper(ctx, c.run, c.topLevelFlags, f.Args())
}

func (c *projectConfigCmd) run(jirix *jiri.X, args []string) error {
	p, err := currentProject(jirix)
	if err != nil {
		return err
	}
	if c.ignore == "" && c.noUpdate == "" && c.noRebase == "" {
		displayConfig(jirix, p.LocalConfig)
		return nil
	}
	lc := p.LocalConfig
	if err := setBoolVar(c.ignore, &lc.Ignore, "ignore"); err != nil {
		return err
	}
	if err := setBoolVar(c.noUpdate, &lc.NoUpdate, "no-update"); err != nil {
		return err
	}
	if err := setBoolVar(c.noRebase, &lc.NoRebase, "no-rebase"); err != nil {
		return err
	}
	return project.WriteLocalConfig(jirix, p, lc)
}

func setBoolVar(value string, b *bool, flagName string) error {
	if value == "" {
		return nil
	}
	if val, err := strconv.ParseBool(value); err != nil {
		return fmt.Errorf("%s flag should be true or false", flagName)
	} else {
		*b = val
	}
	return nil
}

func displayConfig(jirix *jiri.X, lc project.LocalConfig) {
	fmt.Fprintf(jirix.Stdout(), "Config:\n")
	fmt.Fprintf(jirix.Stdout(), "ignore: %t\n", lc.Ignore)
	fmt.Fprintf(jirix.Stdout(), "no-update: %t\n", lc.NoUpdate)
	fmt.Fprintf(jirix.Stdout(), "no-rebase: %t\n", lc.NoRebase)
}
