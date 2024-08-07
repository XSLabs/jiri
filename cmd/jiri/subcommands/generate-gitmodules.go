// Copyright 2019 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package subcommands

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/subcommands"
	"go.fuchsia.dev/jiri"
	"go.fuchsia.dev/jiri/project"
)

// TODO(https://fxbug.dev/356134056): delete when finished migrating to
// subcommands library.
var (
	genGitModuleFlags genGitModuleCmd
	cmdGenGitModule   = commandFromSubcommand(&genGitModuleFlags)
)

// TODO(https://fxbug.dev/356134056): delete when finished migrating to
// subcommands library.
func init() {
	genGitModuleFlags.SetFlags(&cmdGenGitModule.Flags)
}

type genGitModuleCmd struct {
	genScript    string
	redirectRoot bool
}

func (c *genGitModuleCmd) Name() string { return "generate-gitmodules" }
func (c *genGitModuleCmd) Synopsis() string {
	return "Create a .gitmodule and a .gitattributes files for git submodule repository"
}
func (c *genGitModuleCmd) Usage() string {
	return `
The "jiri generate-gitmodules command captures the current project state and
create a .gitmodules file and an optional .gitattributes file for building
a git submodule based super repository.

Usage:
  jiri generate-gitmodules [flags] < .gitmodule path> [<.gitattributes path>]

<.gitmodule path> is the path to the output .gitmodule file.
<.gitattributes path> is the path to the output .gitattribute file, which is optional.
`
}

func (c *genGitModuleCmd) SetFlags(f *flag.FlagSet) {
	f.StringVar(&c.genScript, "generate-script", "", "File to save generated git commands for seting up a superproject.")
	f.BoolVar(&c.redirectRoot, "redir-root", false, "When set to true, jiri will add the root repository as a submodule into {name}-mirror directory and create necessary setup commands in generated script.")
}

func (c *genGitModuleCmd) Execute(ctx context.Context, _ *flag.FlagSet, args ...any) subcommands.ExitStatus {
	return executeWrapper(ctx, c.run, args)
}

func (c *genGitModuleCmd) run(jirix *jiri.X, args []string) error {
	gitmodulesPath := ".gitmodules"
	gitattributesPath := ""
	if len(args) >= 1 {
		gitmodulesPath = args[0]
	}
	if len(args) == 2 {
		gitattributesPath = args[1]
	}

	if len(args) > 2 {
		return jirix.UsageErrorf("unexpected number of arguments")
	}

	localProjects, err := project.LocalProjects(jirix, project.FullScan)
	if err != nil {
		return err
	}
	return c.writeGitModules(jirix, localProjects, gitmodulesPath, gitattributesPath)
}

func (c *genGitModuleCmd) writeGitModules(jirix *jiri.X, projects project.Projects, gitmodulesPath, gitattributesPath string) error {
	projEntries, treeRoot, err := project.GenerateSubmoduleTree(jirix, projects)
	if err != nil {
		return err
	}

	// Start creating .gitmodule and set up script.
	var gitmoduleBuf bytes.Buffer
	var commandBuf bytes.Buffer
	var gitattributeBuf bytes.Buffer
	commandBuf.WriteString("#!/bin/sh\n")

	// Special hack for fuchsia.git
	// When -redir-root is set to true, fuchsia.git will be added as submodule
	// to fuchsia-mirror directory
	reRootRepoName := ""
	if c.redirectRoot {
		// looking for root repository, there should be no more than 1
		rIndex := -1
		for i, v := range projEntries {
			if v.Path == "." || v.Path == "" || v.Path == string(filepath.Separator) {
				if rIndex == -1 {
					rIndex = i
				} else {
					return fmt.Errorf("more than 1 project defined at path \".\", projects %+v:%+v", projEntries[rIndex], projEntries[i])
				}
			}
		}
		if rIndex != -1 {
			v := projEntries[rIndex]
			v.Name = v.Name + "-mirror"
			v.Path = v.Name
			reRootRepoName = v.Path
			gitmoduleBuf.WriteString(moduleDecl(v))
			gitmoduleBuf.WriteString("\n")
			commandBuf.WriteString(commandDecl(v))
			commandBuf.WriteString("\n")
			if v.GitAttributes != "" {
				gitattributeBuf.WriteString(attributeDecl(v))
				gitattributeBuf.WriteString("\n")
			}
		}
	}

	for _, v := range projEntries {
		if reRootRepoName != "" && reRootRepoName == v.Path {
			return fmt.Errorf("path collision for root repo and project %+v", v)
		}
		if _, ok := treeRoot.Dropped[v.Key()]; ok {
			jirix.Logger.Debugf("dropped project %+v", v)
			continue
		}
		gitmoduleBuf.WriteString(moduleDecl(v))
		gitmoduleBuf.WriteString("\n")
		commandBuf.WriteString(commandDecl(v))
		commandBuf.WriteString("\n")
		if v.GitAttributes != "" {
			gitattributeBuf.WriteString(attributeDecl(v))
			gitattributeBuf.WriteString("\n")
		}
	}
	jirix.Logger.Debugf("generated gitmodule content \n%v\n", gitmoduleBuf.String())
	if err := os.WriteFile(gitmodulesPath, gitmoduleBuf.Bytes(), 0644); err != nil {
		return err
	}

	if c.genScript != "" {
		jirix.Logger.Debugf("generated set up script for gitmodule content \n%v\n", commandBuf.String())
		if err := os.WriteFile(c.genScript, commandBuf.Bytes(), 0755); err != nil {
			return err
		}
	}

	if gitattributesPath != "" {
		jirix.Logger.Debugf("generated gitattributes content \n%v\n", gitattributeBuf.String())
		if err := os.WriteFile(gitattributesPath, gitattributeBuf.Bytes(), 0644); err != nil {
			return err
		}
	}
	return nil
}

type projectTree struct {
	project  *project.Project
	children map[string]*projectTree
}

type projectTreeRoot struct {
	root    *projectTree
	dropped project.Projects
}

func makePathRel(basepath, targpath string) (string, error) {
	if filepath.IsAbs(targpath) {
		relPath, err := filepath.Rel(basepath, targpath)
		if err != nil {
			return "", err
		}
		return relPath, nil
	}
	return targpath, nil
}

func moduleDecl(p project.Project) string {
	lines := []string{fmt.Sprintf("[submodule \"%s\"]", p.Path)}
	if p.Name != "" {
		lines = append(lines, "name = "+p.Name)
	}
	lines = append(lines, "path = "+p.Path)
	lines = append(lines, "url = "+p.Remote)
	return strings.Join(lines, "\n\t")
}

func commandDecl(p project.Project) string {
	tmpl := "git update-index --add --cacheinfo 160000 %s \"%s\""
	return fmt.Sprintf(tmpl, p.Revision, p.Path)
}

func attributeDecl(p project.Project) string {
	tmpl := "%s %s"
	attrs := strings.ReplaceAll(p.GitAttributes, ",", " ")
	return fmt.Sprintf(tmpl, p.Path, strings.TrimSpace(attrs))
}
