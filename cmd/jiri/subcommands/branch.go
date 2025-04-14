// Copyright 2017 The Fuchsia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package subcommands

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/google/subcommands"
	"go.fuchsia.dev/jiri"
	"go.fuchsia.dev/jiri/gerrit"
	"go.fuchsia.dev/jiri/gitutil"
	"go.fuchsia.dev/jiri/project"
)

type branchCmd struct {
	cmdBase
	delete                bool
	deleteMergedCLs       bool
	deleteMerged          bool
	forceDelete           bool
	overrideProjectConfig bool
}

func (c *branchCmd) Name() string     { return "branch" }
func (c *branchCmd) Synopsis() string { return "Show or delete branches" }
func (c *branchCmd) Usage() string {
	return `Show all the projects having branch <branch>. If -d or -D is passed, <branch>
is deleted. if <branch> is not passed, show all projects which have branches other than "main"

Usage:
  jiri branch [flags] <branch>

<branch> is the name of the branch.
`
}

func (c *branchCmd) SetFlags(f *flag.FlagSet) {
	f.BoolVar(&c.delete, "d", false, "Delete branch from project. Similar to running 'git branch -d <branch-name>'")
	f.BoolVar(&c.forceDelete, "D", false, "Force delete branch from project. Similar to running 'git branch -D <branch-name>'")
	f.BoolVar(&c.overrideProjectConfig, "override-pc", false, "Overrides project config's ignore and noupdate flag and deletes the branch.")
	f.BoolVar(&c.deleteMerged, "delete-merged", false, "Delete merged branches. Merged branches are the tracked branches merged with their tracking remote or un-tracked branches merged with the branch specified in manifest(default main). If <branch> is provided, it will only delete branch <branch> if merged.")
	f.BoolVar(&c.deleteMergedCLs, "delete-merged-cl", false, "Implies -delete-merged. It also parses commit messages for ChangeID and checks with gerrit if those changes have been merged and deletes those branches. It will ignore a branch if it differs with remote by more than 10 commits.")
}

func (c *branchCmd) Execute(ctx context.Context, f *flag.FlagSet, _ ...any) subcommands.ExitStatus {
	return executeWrapper(ctx, c.run, c.topLevelFlags, f.Args())
}

func (c *branchCmd) run(jirix *jiri.X, args []string) error {
	branch := ""
	if len(args) > 1 {
		return jirix.UsageErrorf("Please provide only one branch")
	} else if len(args) == 1 {
		branch = args[0]
	}
	if c.delete || c.forceDelete {
		if branch == "" {
			return jirix.UsageErrorf("Please provide branch to delete")
		}
		return c.deleteBranches(jirix, branch)
	}
	if c.deleteMergedCLs {
		return c.deleteMergedBranches(jirix, branch, true)
	}
	if c.deleteMerged {
		return c.deleteMergedBranches(jirix, branch, false)
	}
	return c.displayProjects(jirix, branch)
}

func (c *branchCmd) displayProjects(jirix *jiri.X, branch string) error {
	localProjects, err := project.LocalProjects(jirix, project.FastScan)
	if err != nil {
		return err
	}
	jirix.TimerPush("Get states")
	states, err := project.GetProjectStates(jirix, localProjects, false)
	if err != nil {
		return err
	}

	jirix.TimerPop()
	cDir := jirix.Cwd
	var keys project.ProjectKeys
	for key := range states {
		keys = append(keys, key)
	}
	sort.Sort(keys)
	for _, key := range keys {
		state := states[key]
		relativePath, err := filepath.Rel(cDir, state.Project.Path)
		if err != nil {
			return err
		}
		if branch == "" {
			var branches []string
			main := ""
			for _, b := range state.Branches {
				name := b.Name
				if state.CurrentBranch.Name == b.Name {
					name = "*" + jirix.Color.Green("%s", b.Name)
				}
				if b.Name != "main" {
					branches = append(branches, name)
				} else {
					main = name
				}
			}
			if len(branches) != 0 {
				if main != "" {
					branches = append(branches, main)
				}
				fmt.Fprintf(jirix.Stdout(), "%s: %s(%s)\n", jirix.Color.Yellow("Project"), state.Project.Name, relativePath)
				fmt.Fprintf(jirix.Stdout(), "%s: %s\n\n", jirix.Color.Yellow("Branch(es)"), strings.Join(branches, ", "))
			}
		} else {
			for _, b := range state.Branches {
				if b.Name == branch {
					fmt.Fprintf(jirix.Stdout(), "%s(%s)\n", state.Project.Name, relativePath)
					break
				}
			}
		}
	}
	jirix.TimerPop()
	return nil
}

var (
	changeIDRE = regexp.MustCompile("Change-Id: (I[0123456789abcdefABCDEF]{40})")
)

func (c *branchCmd) deleteMergedBranches(jirix *jiri.X, branchToDelete string, deleteMergedCls bool) error {
	localProjects, err := project.LocalProjects(jirix, project.FastScan)
	if err != nil {
		return err
	}

	cDir := jirix.Cwd

	jirix.TimerPush("Get states")
	states, err := project.GetProjectStates(jirix, localProjects, false)
	if err != nil {
		return err
	}
	jirix.TimerPop()

	remoteProjects, _, _, err := project.LoadManifestFile(jirix, jirix.JiriManifestFile(), localProjects, nil)
	if err != nil {
		return err
	}

	jirix.TimerPush("Process")
	processProject := func(key project.ProjectKey) {
		state, _ := states[key]
		remote, ok := remoteProjects[key]
		relativePath, err := filepath.Rel(cDir, state.Project.Path)
		if err != nil {
			relativePath = state.Project.Path
		}
		if !c.overrideProjectConfig && (state.Project.LocalConfig.Ignore || state.Project.LocalConfig.NoUpdate) {
			jirix.Logger.Warningf(" Not processing project %s(%s) due to its local-config. Use '-override-pc' flag\n\n", state.Project.Name, state.Project.Path)
			return
		}
		if !ok {
			jirix.Logger.Debugf("Not processing project %s(%s) as it was not found in manifest\n\n", state.Project.Name, relativePath)
			return
		}

		deletedBranches, multiErr := c.deleteProjectMergedBranches(jirix, state.Project, remote, relativePath, branchToDelete)
		if deleteMergedCls {
			deletedBranches2, err2 := c.deleteProjectMergedClsBranches(jirix, state.Project, remote, relativePath, branchToDelete)
			for b, h := range deletedBranches2 {
				deletedBranches[b] = h
			}
			multiErr = errors.Join(multiErr, err2)
		}

		if len(deletedBranches) != 0 || multiErr != nil {
			buf := fmt.Sprintf("Project: %s(%s)\n", state.Project.Name, relativePath)
			if len(deletedBranches) != 0 {
				dbs := []string{}
				for b, h := range deletedBranches {
					dbs = append(dbs, fmt.Sprintf("%s(%s)", b, h))
				}
				buf = buf + fmt.Sprintf("%s: %s\n", jirix.Color.Green("Deleted branch(es)"), strings.Join(dbs, ", "))

				if _, ok := deletedBranches[state.CurrentBranch.Name]; ok {
					buf = buf + fmt.Sprintf("Current branch \"%s\" was deleted and project was put on JIRI_HEAD\n", jirix.Color.Yellow(state.CurrentBranch.Name))
				}
			}
			if multiErr != nil {
				jirix.IncrementFailures()
				buf = buf + fmt.Sprintf("%s\n", multiErr)
				jirix.Logger.Errorf("%s\n", buf)
			} else {
				jirix.Logger.Infof("%s\n", buf)
			}
		}
	}

	workQueue := make(chan project.ProjectKey, len(states))
	for key := range states {
		workQueue <- key
	}
	close(workQueue)

	var wg sync.WaitGroup
	for i := uint(0); i < jirix.Jobs; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for key := range workQueue {
				processProject(key)
			}
		}()
	}

	wg.Wait()
	jirix.TimerPop()

	if jirix.Failures() != 0 {
		return fmt.Errorf("Branch deletion completed with non-fatal errors.")
	}
	return nil
}

func (c *branchCmd) deleteProjectMergedClsBranches(jirix *jiri.X, local project.Project, remote project.Project, relativePath, branchToDelete string) (map[string]string, error) {
	deletedBranches := make(map[string]string)
	var retErr error
	if remote.GerritHost == "" {
		return nil, nil
	}
	hostURL, err := url.Parse(remote.GerritHost)
	if err != nil {
		retErr = errors.Join(retErr, err)
		return nil, retErr
	}
	gerrit := gerrit.New(jirix, hostURL)
	scm := gitutil.New(jirix, gitutil.RootDirOpt(local.Path))
	branches, err := scm.GetAllBranchesInfo()
	if err != nil {
		retErr = errors.Join(retErr, err)
		return nil, retErr
	}
	for _, b := range branches {
		if branchToDelete != "" && b.Name != branchToDelete {
			continue
		}
		// Only show this message when project has some local branch
		if strings.HasPrefix(local.Remote, "sso://") {
			jirix.Logger.Warningf("Skipping project %s(%s) as it uses sso protocol. Not querying gerrit\n\n", local.Name, relativePath)
			return nil, nil
		}
		if b.IsHead {
			untracked, err := scm.HasUntrackedFiles()
			if err != nil {
				retErr = errors.Join(retErr, fmt.Errorf("Not deleting current branch %q as can't get changes: %s\n", b.Name, err))
				continue
			}
			uncommitted, err := scm.HasUncommittedChanges()
			if err != nil {
				retErr = errors.Join(retErr, fmt.Errorf("Not deleting current branch %q as can't get changes: %s\n", b.Name, err))
				continue
			}
			if untracked || uncommitted {
				jirix.Logger.Debugf("Not deleting current branch %q for project %s(%s) as it has changes\n\n", b.Name, local.Name, relativePath)
				continue
			}
		}

		trackingBranch := ""
		if b.Tracking == nil {
			rb := remote.RemoteBranch
			if rb == "" {
				rb = "main"
			}
			trackingBranch = fmt.Sprintf("remotes/origin/%s", rb)
		} else {
			trackingBranch = b.Tracking.Name
		}

		extraCommits, err := scm.ExtraCommits(b.Name, trackingBranch)
		if err != nil {
			retErr = errors.Join(retErr, fmt.Errorf("Not deleting branch %q as can't get extra commits: %s\n", b.Name, err))
			continue
		}

		if len(extraCommits) > 10 {
			jirix.Logger.Debugf("Not deleting branch %q for project %s(%s) as it has more than 10 extra commits\n\n", b.Name, local.Name, relativePath)
			continue
		}

		deleteBranch := true
		for _, c := range extraCommits {
			deleteBranch = false
			log, err := scm.CommitMsg(c)
			if err != nil {
				retErr = errors.Join(retErr, fmt.Errorf("Not deleting branch %q as can't get log for rev %q: %s\n", b.Name, c, err))
				break
			}
			changeID := changeIDRE.FindStringSubmatch(log)
			if len(changeID) != 2 {
				// Invalid/No Changeid
				break
			}
			c, err := gerrit.GetChangeByID(changeID[1])
			if err != nil {
				retErr = errors.Join(retErr, fmt.Errorf("Not deleting branch %q as can't get change %q: %s\n", b.Name, changeID[1], err))
				break
			}
			if c == nil || c.Submitted == "" {
				// Not merged
				break
			}
			deleteBranch = true
		}
		if !deleteBranch {
			continue
		}

		recurseSubmodules := gitutil.RecurseSubmodulesOpt(remote.GitSubmodules && jirix.EnableSubmodules)

		if b.IsHead {
			revision, err := project.GetHeadRevision(remote)
			if err != nil {
				retErr = errors.Join(retErr, fmt.Errorf("Not deleting current branch %q as can't get head revision: %s\n", b.Name, err))
				continue
			}
			if err := scm.Checkout(revision, recurseSubmodules, gitutil.DetachOpt(true)); err != nil {
				retErr = errors.Join(retErr, fmt.Errorf("Not deleting current branch %q as can't checkout JIRI_HEAD: %s\n", b.Name, err))
				continue
			}
		}

		shortHash, err := scm.ShortHash(b.Revision)
		if err != nil {
			retErr = errors.Join(retErr, fmt.Errorf("Not deleting current branch %q as can't short hash: %s\n", b.Name, err))
			continue
		}
		if err := scm.DeleteBranch(b.Name, gitutil.ForceOpt(true)); err != nil {
			retErr = errors.Join(retErr, fmt.Errorf("Cannot delete branch %q: %s\n", b.Name, err))
			if b.IsHead {
				if err := scm.Checkout(b.Name, recurseSubmodules); err != nil {
					retErr = errors.Join(retErr, fmt.Errorf("Not able to put project back on branch %q: %s\n", b.Name, err))
				}
			}
			continue
		}
		deletedBranches[b.Name] = shortHash
	}
	return deletedBranches, retErr
}

func (c *branchCmd) deleteProjectMergedBranches(jirix *jiri.X, local project.Project, remote project.Project, relativePath, branchToDelete string) (map[string]string, error) {
	deletedBranches := make(map[string]string)
	var retErr error
	var mergedBranches map[string]bool
	scm := gitutil.New(jirix, gitutil.RootDirOpt(local.Path))
	branches, err := scm.GetAllBranchesInfo()
	if err != nil {
		retErr = errors.Join(retErr, err)
		return nil, retErr
	}
	for _, b := range branches {
		if branchToDelete != "" && b.Name != branchToDelete {
			continue
		}
		deleteForced := false

		if b.Tracking == nil {
			// check if this branch is merged
			if mergedBranches == nil {
				// populate
				mergedBranches = make(map[string]bool)
				rb := remote.RemoteBranch
				if rb == "" {
					rb = "main"
				}
				if mbs, err := scm.MergedBranches("remotes/origin/" + rb); err != nil {
					retErr = errors.Join(retErr, fmt.Errorf("Not able to get merged un-tracked branches: %s\n", err))
					continue
				} else {
					for _, mb := range mbs {
						mergedBranches[mb] = true
					}
				}
			}
			if !mergedBranches[b.Name] {
				continue
			}
			deleteForced = true
		}

		recurseSubmodules := gitutil.RecurseSubmodulesOpt(remote.GitSubmodules && jirix.EnableSubmodules)

		if b.IsHead {
			untracked, err := scm.HasUntrackedFiles()
			if err != nil {
				retErr = errors.Join(retErr, fmt.Errorf("Not deleting current branch %q as can't get changes: %s\n", b.Name, err))
				continue
			}
			uncommitted, err := scm.HasUncommittedChanges()
			if err != nil {
				retErr = errors.Join(retErr, fmt.Errorf("Not deleting current branch %q as can't get changes: %s\n", b.Name, err))
				continue
			}
			if untracked || uncommitted {
				jirix.Logger.Debugf("Not deleting current branch %q for project %s(%s) as it has changes\n\n", b.Name, local.Name, relativePath)
				continue
			}
			revision, err := project.GetHeadRevision(remote)
			if err != nil {
				retErr = errors.Join(retErr, fmt.Errorf("Not deleting current branch %q as can't get head revision: %s\n", b.Name, err))
				continue
			}
			if err := scm.Checkout(revision, recurseSubmodules, gitutil.DetachOpt(true)); err != nil {
				retErr = errors.Join(retErr, fmt.Errorf("Not deleting current branch %q as can't checkout JIRI_HEAD: %s\n", b.Name, err))
				continue
			}
		}

		shortHash, err := scm.ShortHash(b.Revision)
		if err != nil {
			retErr = errors.Join(retErr, fmt.Errorf("Not deleting current branch %q as can't short hash: %s\n", b.Name, err))
			continue
		}
		if err := scm.DeleteBranch(b.Name, gitutil.ForceOpt(deleteForced)); err != nil {
			if deleteForced {
				retErr = errors.Join(retErr, fmt.Errorf("Cannot delete branch %q: %s\n", b.Name, err))
			}
			if b.IsHead {
				if err := scm.Checkout(b.Name, recurseSubmodules); err != nil {
					retErr = errors.Join(retErr, fmt.Errorf("Not able to put project back on branch %q: %s\n", b.Name, err))
				}
			}
			continue
		}
		deletedBranches[b.Name] = shortHash
	}
	return deletedBranches, retErr
}

func (c *branchCmd) deleteBranches(jirix *jiri.X, branchToDelete string) error {
	localProjects, err := project.LocalProjects(jirix, project.FastScan)
	if err != nil {
		return err
	}
	cDir := jirix.Cwd
	states, err := project.GetProjectStates(jirix, localProjects, false)
	if err != nil {
		return err
	}

	jirix.TimerPush("Process")
	errors := false
	projectFound := false
	var keys project.ProjectKeys
	for key := range states {
		keys = append(keys, key)
	}
	sort.Sort(keys)
	for _, key := range keys {
		state := states[key]
		for _, branch := range state.Branches {
			if branch.Name == branchToDelete {
				projectFound = true
				localProject := state.Project
				relativePath, err := filepath.Rel(cDir, localProject.Path)
				if err != nil {
					return err
				}
				if !c.overrideProjectConfig && (localProject.LocalConfig.Ignore || localProject.LocalConfig.NoUpdate) {
					jirix.Logger.Warningf("Project %s(%s): branch %q won't be deleted due to its local-config. Use '-override-pc' flag\n\n", localProject.Name, localProject.Path, branchToDelete)
					break
				}
				fmt.Fprintf(jirix.Stdout(), "Project %s(%s): ", localProject.Name, relativePath)
				scm := gitutil.New(jirix, gitutil.RootDirOpt(localProject.Path))

				if err := scm.DeleteBranch(branchToDelete, gitutil.ForceOpt(c.forceDelete)); err != nil {
					errors = true
					fmt.Fprintf(jirix.Stdout(), "%s", jirix.Color.Red("Error while deleting branch: %s\n", err))
				} else {
					shortHash, err := scm.ShortHash(branch.Revision)
					if err != nil {
						return err
					}
					fmt.Fprintf(jirix.Stdout(), "%s (was %s)\n", jirix.Color.Green("Deleted Branch %s", branchToDelete), jirix.Color.Yellow(shortHash))
				}
				break
			}
		}
	}
	jirix.TimerPop()

	if !projectFound {
		fmt.Fprintf(jirix.Stdout(), "Cannot find any project with branch %q\n", branchToDelete)
		return nil
	}
	if errors {
		fmt.Fprintln(jirix.Stdout(), jirix.Color.Yellow("Please check errors above"))
	}
	return nil
}
