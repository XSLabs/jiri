// Copyright 2022 The Fuchsia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"go.fuchsia.dev/jiri"
	"go.fuchsia.dev/jiri/cmdline"
	"go.fuchsia.dev/jiri/gitutil"
	"go.fuchsia.dev/jiri/project"
)

var cmdCheckClean = &cmdline.Command{
	Runner: jiri.RunnerFunc(runCheckClean),
	Name:   "check-clean",
	Short:  "Checks if the checkout is clean",
	Long: `
Exits non-zero and prints repositories (and their status) if they contain
uncommitted changes.
`,
}

func runCheckClean(jirix *jiri.X, args []string) error {
	localProjects, err := project.LocalProjects(jirix, project.FastScan)
	if err != nil {
		return err
	}
	cDir, err := os.Getwd()
	if err != nil {
		return err
	}
	states, err := project.GetProjectStates(jirix, localProjects, false)
	if err != nil {
		return err
	}
	var keys project.ProjectKeys
	for key := range localProjects {
		keys = append(keys, key)
	}
	sort.Sort(keys)
	dirtyProjects := make(map[string]string)
	for _, key := range keys {
		localProject := localProjects[key]
		state, ok := states[key]
		if !ok {
			// this should not happen
			panic(fmt.Sprintf("State not found for project %q", localProject.Name))
		}
		if statusFlags.branch != "" && (statusFlags.branch != state.CurrentBranch.Name) {
			continue
		}
		relativePath, err := filepath.Rel(cDir, localProject.Path)
		if err != nil {
			return err
		}
		scm := gitutil.New(jirix, gitutil.RootDirOpt(localProject.Path))
		changes, err := scm.ShortStatus()
		if err != nil {
			jirix.Logger.Errorf("%s :%s\n\n", fmt.Sprintf("getting changes for project %s(%s)", localProject.Name, relativePath), err)
			jirix.IncrementFailures()
			continue
		}
		if changes != "" {
			dirtyProjects[relativePath] = changes
		}
	}
	var finalErr error
	if jirix.Failures() != 0 {
		finalErr = fmt.Errorf("completed with non-fatal errors")
	} else if len(dirtyProjects) > 0 {
		finalErr = fmt.Errorf("Checkout is not clean!")
	}

	if len(dirtyProjects) > 0 {
		fmt.Println("Dirty projects:")
		for relativePath, changes := range dirtyProjects {
			fmt.Printf("%s\n%s\n\n", relativePath, changes)
		}
	}

	return finalErr
}
