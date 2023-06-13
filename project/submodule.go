// Copyright 2017 The Fuchsia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package project

import (
	"fmt"
	"path/filepath"
	"regexp"

	"go.fuchsia.dev/jiri"
	"go.fuchsia.dev/jiri/gitutil"
)

type Submodule struct {
	Name string
	Path string
	// Submodule SHA-1 prefix, could be "+", "-" or "U".
	// "-" means submodule not initialized.
	Prefix       string
	Remote       string
	Revision     string
	Superproject string
}

type Submodules map[string]Submodule

var submoduleConfigRegex = regexp.MustCompile(`([-+U]?)([a-fA-F0-9]{40})\s([^\s]*)\s?`)

// containSubmodules checks if any of the projects contain submodules.
func containSubmodules(jirix *jiri.X, projects Projects) bool {
	for _, p := range projects {
		if p.GitSubmodules {
			if isSuperproject(jirix, p) {
				return true
			}
		}
	}
	return false
}

// containLocalSubmodules checks if any projects has IsSubmodule flagged as true.
// If yes, it means that the project current state exist as a submodule.
func containLocalSubmodules(projects Projects) bool {
	for _, p := range projects {
		if p.IsSubmodule {
			return true
		}
	}
	return false
}

func createBranchSubmodules(jirix *jiri.X, superproject Project, branch string) error {
	submStates, err := getSubmodulesStatus(jirix, superproject)
	if err != nil {
		return err
	}
	for _, subm := range submStates {
		if subm.Prefix == "-" {
			continue
		}
		scm := gitutil.New(jirix, gitutil.RootDirOpt(subm.Path))
		if err := scm.CreateBranchFromRef(branch, "origin/HEAD"); err != nil {
			return err
		}
	}
	return nil
}

// getAllSubmodules return all submodules states.
func getAllSubmodules(jirix *jiri.X, projects Projects) []Submodules {
	var allSubmodules []Submodules
	for _, p := range projects {
		if p.GitSubmodules {
			if submodules, _ := getSubmodulesStatus(jirix, p); submodules != nil {
				allSubmodules = append(allSubmodules, submodules)
			}
		}
	}
	return allSubmodules
}

// getSubmoduleStatus returns submodule states in superproject.
func getSubmodulesStatus(jirix *jiri.X, superproject Project) (Submodules, error) {
	scm := gitutil.New(jirix, gitutil.RootDirOpt(superproject.Path))
	submoduleStatus, _ := scm.SubmoduleStatus()
	submodules := make(Submodules)
	for _, submodule := range submoduleStatus {
		submConfig := submoduleConfigRegex.FindStringSubmatch(submodule)
		if len(submConfig) != 4 {
			return nil, fmt.Errorf("expected substring to have length of 4, but got %d", len(submConfig))
		}
		subm := Submodule{
			Prefix:       submConfig[1],
			Revision:     submConfig[2],
			Path:         submConfig[3],
			Superproject: superproject.Name,
		}
		subm.Remote, _ = scm.SubmoduleConfig(subm.Path, "url")
		subm.Name, _ = scm.SubmoduleConfig(subm.Path, "name")
		subm.Path = filepath.Join(superproject.Path, submConfig[3])
		submodules[subm.Name] = subm
		if subm.Prefix == "+" {
			jirix.Logger.Warningf("Submodule %s current checkout does not match the SHA-1 to the index of the containing repository.", subm.Name)
		}
		if subm.Prefix == "U" {
			jirix.Logger.Warningf("Submodule %s has merge conflicts.", subm.Name)
		}
	}
	return submodules, nil
}

// getSuperprojectStates returns the superprojects that have submodules enabled based on manifest.
func getSuperprojectStates(projects Projects) map[string]Project {
	superprojectStates := make(map[string]Project)
	for _, p := range projects {
		if p.GitSubmodules {
			superprojectStates[p.Name] = p
		}
	}
	return superprojectStates
}

// isSuperproject checks if submodules exist under a project
func isSuperproject(jirix *jiri.X, project Project) bool {
	submodules, _ := getSubmodulesStatus(jirix, project)
	for _, subm := range submodules {
		if subm.Prefix != "-" {
			return true
		}
	}
	return false
}

// removeSubmodulesFromProjects removes verified submodules from jiri projects.
func removeSubmodulesFromProjects(projects Projects) Projects {
	var submoduleProjectKeys []ProjectKey
	superprojectStates := getSuperprojectStates(projects)
	for k, p := range projects {
		if _, ok := superprojectStates[p.GitSubmoduleOf]; ok {
			submoduleProjectKeys = append(submoduleProjectKeys, k)
		}
	}
	for _, k := range submoduleProjectKeys {
		delete(projects, k)
	}
	return projects
}

// cleanSubmoduleSentinalBranches removes the sentinal branch from submodules.
// This ensures a healthy state even after a failed `jiri update`.
func cleanSubmoduleSentinalBranches(jirix *jiri.X, superproject Project, sentinalBranch string) error {
	if !superproject.GitSubmodules {
		return nil
	}
	submStates, _ := getSubmodulesStatus(jirix, superproject)
	for _, subm := range submStates {
		if subm.Prefix == "-" {
			continue
		}
		scm := gitutil.New(jirix, gitutil.RootDirOpt(subm.Path))
		if exist, _ := scm.CheckBranchExists(sentinalBranch); exist {
			if err := scm.DeleteBranch(sentinalBranch, gitutil.ForceOpt(true)); err != nil {
				return err
			}
		}
	}
	return nil
}

// removeSubmoduleBranches removes initial branches from submodules.
// We create a local sentinal branch in all submodules first before running update.
// If submodules were created for the first time, "local-submodule-sentinal" branch would not exist. We remove all branches.
// Otherwise, submodules pre-exist the update, then we remove on the dummy branch.
func removeSubmoduleBranches(jirix *jiri.X, superproject Project, sentinalBranch string) error {
	if !superproject.GitSubmodules {
		return nil
	}
	submStates, _ := getSubmodulesStatus(jirix, superproject)
	for _, subm := range submStates {
		if subm.Prefix == "-" {
			continue
		}
		scm := gitutil.New(jirix, gitutil.RootDirOpt(subm.Path))
		if exist, _ := scm.CheckBranchExists(sentinalBranch); !exist {
			branches, _, _ := scm.GetBranches()
			for _, b := range branches {
				if err := scm.DeleteBranch(b); err != nil {
					jirix.Logger.Warningf("not able to delete branch %s for superproject %s(%s)\n\n", b, superproject.Name, superproject.Path)
					return err
				}
			}
		} else {
			if err := scm.DeleteBranch(sentinalBranch, gitutil.ForceOpt(true)); err != nil {
				return err
			}
		}
	}
	return nil
}

// removeAllSubmoduleBranches removes all branches for submodules.
func removeAllSubmoduleBranches(jirix *jiri.X, superproject Project) error {
	if !superproject.GitSubmodules {
		return nil
	}
	submStates, _ := getSubmodulesStatus(jirix, superproject)
	for _, subm := range submStates {
		if subm.Prefix == "-" {
			continue
		}
		scm := gitutil.New(jirix, gitutil.RootDirOpt(subm.Path))
		branches, _, _ := scm.GetBranches()
		for _, b := range branches {
			if err := scm.DeleteBranch(b); err != nil {
				jirix.Logger.Warningf("not able to delete branch %s for superproject %s(%s)\n\n", b, superproject.Name, superproject.Path)
				return err
			}
		}
	}
	return nil
}

// submodulesToProject converts submodules to project map with path as key.
func submoduleToProject(submodules Submodules, initOnly bool) map[string]Project {
	projects := make(map[string]Project)
	for _, subm := range submodules {
		// When initOnly is flagged, we only include submodules that are initialized.
		if initOnly && subm.Prefix == "-" {
			continue
		}
		project := Project{
			Name:        subm.Name,
			Path:        subm.Path,
			Remote:      subm.Remote,
			Revision:    subm.Revision,
			IsSubmodule: true,
		}
		projects[project.Path] = project
	}
	return projects
}
