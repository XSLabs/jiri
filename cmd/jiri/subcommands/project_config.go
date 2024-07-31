// Copyright 2017 The Fuchsia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package subcommands

import (
	"fmt"
	"strconv"

	"go.fuchsia.dev/jiri"
	"go.fuchsia.dev/jiri/cmdline"
	"go.fuchsia.dev/jiri/project"
)

var cmdProjectConfig = &cmdline.Command{
	Runner: jiri.RunnerFunc(runProjectConfig),
	Name:   "project-config",
	Short:  "Prints/sets project's local config",
	Long: `
Prints/Manages local project config. This command should be run from inside a
project. It will print config if no flags are provided otherwise set it.`,
}

var projectConfigFlags struct {
	ignore   string
	noUpdate string
	noRebase string
}

func init() {
	cmdProjectConfig.Flags.StringVar(&projectConfigFlags.ignore, "ignore", "", `This can be true or false. If set to true project would be completely ignored while updating`)
	cmdProjectConfig.Flags.StringVar(&projectConfigFlags.noUpdate, "no-update", "", `This can be true or false. If set to true project won't be updated`)
	cmdProjectConfig.Flags.StringVar(&projectConfigFlags.noRebase, "no-rebase", "", `This can be true or false. If set to true local branch won't be rebased or merged.`)
}

func runProjectConfig(jirix *jiri.X, args []string) error {
	p, err := currentProject(jirix)
	if err != nil {
		return err
	}
	if projectConfigFlags.ignore == "" && projectConfigFlags.noUpdate == "" && projectConfigFlags.noRebase == "" {
		displayConfig(jirix, p.LocalConfig)
		return nil
	}
	lc := p.LocalConfig
	if err := setBoolVar(projectConfigFlags.ignore, &lc.Ignore, "ignore"); err != nil {
		return err
	}
	if err := setBoolVar(projectConfigFlags.noUpdate, &lc.NoUpdate, "no-update"); err != nil {
		return err
	}
	if err := setBoolVar(projectConfigFlags.noRebase, &lc.NoRebase, "no-rebase"); err != nil {
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
