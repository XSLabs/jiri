// Copyright 2017 The Fuchsia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package project

import (
	"fmt"
	"hash/fnv"
	"io"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"

	"go.fuchsia.dev/jiri"
	"go.fuchsia.dev/jiri/gitutil"
	"go.fuchsia.dev/jiri/log"
	"go.fuchsia.dev/jiri/osutil"
)

const (
	changeRemoteOpKind = "change-remote"
	createOpKind       = "create"
	deleteOpKind       = "delete"
	moveOpKind         = "move"
	nullOpKind         = "null"
	updateOpKind       = "update"
)

type operation interface {
	// Project identifies the project this operation pertains to.
	Project() Project
	// Kind returns the kind of operation.
	Kind() string
	// Run executes the operation.
	Run(jirix *jiri.X) error
	// String returns a string representation of the operation.
	String() string
	// Test checks whether the operation would fail.
	Test(jirix *jiri.X) error
	// Source returns the original path of the Project.
	Source() string
	// Destination returns the future path of the Project.
	Destination() string
}

// commonOperation represents a project operation.
type commonOperation struct {
	// project holds information about the project such as its
	// name, local path, and the protocol it uses for version
	// control.
	project Project
	// destination is the new project path.
	destination string
	// source is the current project path.
	source string
	// state is the state of the local project
	state ProjectState
}

func (op commonOperation) Project() Project {
	return op.project
}

func (op commonOperation) Source() string {
	return op.source
}

func (op commonOperation) Destination() string {
	return op.destination
}

// createOperation represents the creation of a project.
type createOperation struct {
	commonOperation
}

func (op createOperation) Kind() string {
	return createOpKind
}

func (op createOperation) checkoutProject(jirix *jiri.X, cache string) error {
	var err error
	remote := rewriteRemote(jirix, op.project.Remote)
	scm := gitutil.New(jirix, gitutil.RootDirOpt(op.project.Path))
	// Hack to make fuchsia.git happen
	if op.destination == jirix.Root {
		if err = scm.Init(op.destination); err != nil {
			return err
		}
		if err = scm.AddOrReplaceRemote("origin", remote); err != nil {
			return err
		}
		// This appears to be set to 0 via some quirk of `git init`.
		if err := scm.Config("core.repositoryformatversion", "1"); err != nil {
			return err
		}
		if jirix.UsePartialClone(op.project.Remote) {
			if err := scm.Config("extensions.partialClone", "origin"); err != nil {
				return err
			}
			if err := scm.AddOrReplacePartialRemote("origin", remote); err != nil {
				return err
			}
		}
		// We must specify a refspec here in order for patch to be able to set
		// upstream to 'origin/main'.
		if err := scm.Config("remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*"); err != nil {
			return err
		}
		if cache != "" {
			objPath := "objects"
			if jirix.UsePartialClone(op.project.Remote) {
				objPath = ".git/objects"
			}
			if err := os.WriteFile(filepath.Join(op.destination, ".git/objects/info/alternates"), []byte(filepath.Join(cache, objPath)+"\n"), 0644); err != nil {
				return err
			}
		}
		if err = fetchAll(jirix, op.project); err != nil {
			return err
		}

		if cache != "" && jirix.Dissociate {
			// Dissociating from the cache is slightly more complicated here,
			// as `git fetch` does not have a `--dissociate` flag. As a result,
			// we must invoke a dissociate manually. This involves running a
			// repack, as well as removing the alternatives file. See the
			// implementation of the dissociate flag in
			// https://github.com/git/git/blob/main/builtin/clone.c#L1399 for
			// more details.
			opts := []gitutil.RepackOpt{gitutil.RepackAllOpt(true), gitutil.RemoveRedundantOpt(true)}
			if err := gitutil.New(jirix).Repack(opts...); err != nil {
				return err
			}
			if err := os.Remove(filepath.Join(op.destination, ".git/objects/info/alternates")); err != nil {
				return err
			}
		}
	} else {
		r := remote
		if cache != "" {
			r = cache
			defer func() {
				if err := scm.AddOrReplaceRemote("origin", remote); err != nil {
					jirix.Logger.Errorf("failed to set remote back to %v for project %+v", remote, op.project)
				}
			}()
		}
		opts := []gitutil.CloneOpt{gitutil.NoCheckoutOpt(true)}
		if op.project.HistoryDepth > 0 {
			opts = append(opts, gitutil.DepthOpt(op.project.HistoryDepth))
		} else {
			// Shallow clones can not be used as as local git reference
			opts = append(opts, gitutil.ReferenceOpt(cache))
		}
		// Passing --filter=blob:none for a local clone is a no-op.
		if (cache == r || cache == "") && jirix.UsePartialClone(op.project.Remote) {
			opts = append(opts, gitutil.OmitBlobsOpt(true))
		}
		if jirix.Dissociate {
			opts = append(opts, gitutil.DissociateOpt(true))
		}
		if err = clone(jirix, r, op.destination, opts...); err != nil {
			return err
		}
	}

	if err := os.Chmod(op.destination, os.FileMode(0755)); err != nil {
		return fmtError(err)
	}

	if err := checkoutHeadRevision(jirix, op.project, false); err != nil {
		return err
	}

	if err := writeMetadata(jirix, op.project, op.project.Path); err != nil {
		return err
	}
	// Delete initial branch(es)
	if branches, _, err := scm.GetBranches(); err != nil {
		jirix.Logger.Warningf("not able to get branches for newly created project %s(%s)\n\n", op.project.Name, op.project.Path)
	} else {
		scm := gitutil.New(jirix, gitutil.RootDirOpt(op.project.Path))
		for _, b := range branches {
			if err := scm.DeleteBranch(b); err != nil {
				jirix.Logger.Warningf("not able to delete branch %s for project %s(%s)\n\n", b, op.project.Name, op.project.Path)
			}
		}
	}
	return nil
}

func (op createOperation) Run(jirix *jiri.X) (e error) {
	path, perm := filepath.Dir(op.destination), os.FileMode(0755)

	// Check the local file system.
	if op.destination != jirix.Root {
		if _, err := os.Stat(op.destination); err != nil {
			if !os.IsNotExist(err) {
				return fmtError(err)
			}
		} else {
			if isEmpty, err := isEmpty(op.destination); err != nil {
				return err
			} else if !isEmpty {
				return fmt.Errorf("cannot create %q as it already exists and is not empty", op.destination)
			} else {
				if err := os.RemoveAll(op.destination); err != nil {
					return fmt.Errorf("Not able to delete %q", op.destination)
				}
			}
		}

		if err := os.MkdirAll(path, perm); err != nil {
			return fmtError(err)
		}
	}

	cache, err := op.project.CacheDirPath(jirix)
	if err != nil {
		return err
	}
	if !isPathDir(cache) {
		cache = ""
	}

	if err := op.checkoutProject(jirix, cache); err != nil {
		if op.destination != jirix.Root {
			if err := os.RemoveAll(op.destination); err != nil {
				jirix.Logger.Warningf("Not able to remove %q after create failed: %s", op.destination, err)
			}
		}
		return err
	}

	// Remove branches for submodules if current project is a superproject.
	if jirix.EnableSubmodules && op.project.GitSubmodules {
		if err := removeAllSubmoduleBranches(jirix, op.project); err != nil {
			return err
		}
	}

	return nil
}

func (op createOperation) String() string {
	return fmt.Sprintf("create project %q in %q and advance it to %q", op.project.Name, op.destination, fmtRevision(op.project.Revision))
}

func (op createOperation) Test(jirix *jiri.X) error {
	return nil
}

// deleteOperation represents the deletion of a project.
type deleteOperation struct {
	commonOperation
}

func (op deleteOperation) Kind() string {
	return deleteOpKind
}

func (op deleteOperation) Run(jirix *jiri.X) error {
	if op.project.LocalConfig.Ignore {
		jirix.Logger.Warningf("Project %s(%s) won't be deleted due to its local-config\n\n", op.project.Name, op.source)
		return nil
	}
	// Never delete projects with non-main branches, uncommitted work, or
	// untracked content.
	scm := gitutil.New(jirix, gitutil.RootDirOpt(op.project.Path))
	branches, _, err := scm.GetBranches()
	if err != nil {
		return fmt.Errorf("Cannot get branches for project %q: %s", op.Project().Name, err)
	}
	uncommitted, err := scm.HasUncommittedChanges()
	if err != nil {
		return fmt.Errorf("Cannot get uncommitted changes for project %q: %s", op.Project().Name, err)
	}
	untracked, err := scm.HasUntrackedFiles()
	if err != nil {
		return fmt.Errorf("Cannot get untracked changes for project %q: %s", op.Project().Name, err)
	}
	extraBranches := false
	for _, branch := range branches {
		if !strings.Contains(branch, "HEAD detached") {
			extraBranches = true
			break
		}
	}

	if extraBranches || uncommitted || untracked {
		gitDir, err := op.project.AbsoluteGitDir(jirix)
		if err != nil {
			return err
		}
		rmCommand := jirix.Color.Yellow("rm -rf %q", op.source)
		unManageCommand := jirix.Color.Yellow("rm -rf %q", filepath.Join(gitDir, jiri.ProjectMetaDir))
		msg := ""
		if extraBranches {
			msg = fmt.Sprintf("Project %q won't be deleted as it contains branches", op.project.Name)
		} else {
			msg = fmt.Sprintf("Project %q won't be deleted as it might contain changes", op.project.Name)
		}
		msg += fmt.Sprintf("\nIf you no longer need it, invoke '%s'", rmCommand)
		msg += fmt.Sprintf("\nIf you no longer want jiri to manage it, invoke '%s'\n\n", unManageCommand)
		jirix.Logger.Warningf("%s", msg)
		return nil
	}

	if err := os.RemoveAll(op.source); err != nil {
		return fmtError(err)
	}
	return removeEmptyParents(jirix, path.Dir(op.source))
}

func removeEmptyParents(jirix *jiri.X, dir string) error {
	isEmpty := func(name string) (bool, error) {
		f, err := os.Open(name)
		if err != nil {
			return false, err
		}
		defer f.Close()
		_, err = f.Readdirnames(1)
		if err == io.EOF {
			// empty dir
			return true, nil
		}
		if err != nil {
			return false, err
		}
		return false, nil
	}
	if !strings.HasPrefix(dir, jirix.Root) || jirix.Root == dir || dir == "" || dir == "." {
		return nil
	}
	empty, err := isEmpty(dir)
	if err != nil {
		return err
	}
	if empty {
		if err := os.Remove(dir); err != nil {
			return err
		}
		jirix.Logger.Debugf("gc deleted empty parent directory: %v", dir)
		return removeEmptyParents(jirix, path.Dir(dir))
	}
	return nil
}

func (op deleteOperation) String() string {
	return fmt.Sprintf("delete project %q from %q", op.project.Name, op.source)
}

func (op deleteOperation) Test(jirix *jiri.X) error {
	if _, err := os.Stat(op.source); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("cannot delete %q as it does not exist", op.source)
		}
		return fmtError(err)
	}
	return nil
}

// moveOperation represents the relocation of a project.
type moveOperation struct {
	commonOperation
	rebaseTracked    bool
	rebaseUntracked  bool
	rebaseAll        bool
	rebaseSubmodules bool
	snapshot         bool
}

func (op moveOperation) Kind() string {
	return moveOpKind
}

func (op moveOperation) Run(jirix *jiri.X) error {
	if op.project.LocalConfig.Ignore {
		jirix.Logger.Warningf("Project %s(%s) won't be moved or updated  due to its local-config\n\n", op.project.Name, op.source)
		return nil
	}
	// If it was nested project it might have been moved with its parent project
	if op.source != op.destination {
		if err := renameDir(jirix, op.source, op.destination); err != nil {
			return fmtError(err)
		}
	}
	if err := syncProjectMaster(jirix, op.project, op.state, op.rebaseTracked, op.rebaseUntracked, op.rebaseAll, op.rebaseSubmodules, op.snapshot); err != nil {
		return err
	}
	return writeMetadata(jirix, op.project, op.project.Path)
}

func (op moveOperation) String() string {
	return fmt.Sprintf("move project %q located in %q to %q and advance it to %q", op.project.Name, op.source, op.destination, fmtRevision(op.project.Revision))
}

func (op moveOperation) Test(jirix *jiri.X) error {
	if _, err := os.Stat(op.source); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("cannot move %q to %q as the source does not exist", op.source, op.destination)
		}
		return fmtError(err)
	}

	if _, err := os.Stat(op.destination); err != nil {
		if !os.IsNotExist(err) {
			return fmtError(err)
		}
		// The destination doesn't exist so the move should succeed, no further
		// validation necessary.
		return nil
	}

	// If we get here, it means the destination already exists. This may be
	// acceptable under certain conditions, which we proceed to check for.

	// Check if the project is being moved down a directory, i.e. the
	// destination is a subdirectory of the source.
	// TODO(olivernewman): This is only safe as long as the source directory
	// doesn't contain any nested projects, since any nested projects will also
	// be moved even if their paths were not updated. In practice it should be
	// quite rare to change the path of a project that contains nested projects,
	// so we don't bother handling that case.
	if strings.HasPrefix(op.destination, op.source+string(filepath.Separator)) {
		return nil
	}

	// Check if the project is being moved up a directory.
	files, err := os.ReadDir(op.destination)
	if err != nil {
		return fmtError(err)
	}
	for _, file := range files {
		// If we find any file in the destination directory besides the source
		// directory, it's not safe to move the source directory to the
		// destination.
		if filepath.Join(op.destination, file.Name()) != op.source {
			return fmt.Errorf("cannot move %q to %q as the destination already exists", op.source, op.destination)
		}
	}

	return nil
}

// changeRemoteOperation represents the change of remote URL
type changeRemoteOperation struct {
	commonOperation
	rebaseTracked    bool
	rebaseUntracked  bool
	rebaseAll        bool
	rebaseSubmodules bool
	snapshot         bool
}

func (op changeRemoteOperation) Kind() string {
	return changeRemoteOpKind
}

func (op changeRemoteOperation) Run(jirix *jiri.X) error {
	if op.project.LocalConfig.Ignore || op.project.LocalConfig.NoUpdate {
		jirix.Logger.Warningf("Project %s(%s) won't be updated due to its local-config. It has a changed remote\n\n", op.project.Name, op.project.Path)
		return nil
	}
	git := gitutil.New(jirix, gitutil.RootDirOpt(op.project.Path))
	tempRemote := "new-remote-origin"
	if err := git.AddRemote(tempRemote, op.project.Remote); err != nil {
		return err
	}
	defer git.DeleteRemote(tempRemote)

	if err := fetch(jirix, op.project.Path, tempRemote); err != nil {
		return err
	}

	// Check for all leaf commits in new remote
	for _, branch := range op.state.Branches {
		if containingBranches, err := git.GetRemoteBranchesContaining(branch.Revision); err != nil {
			return err
		} else {
			foundBranch := false
			for _, remoteBranchName := range containingBranches {
				if strings.HasPrefix(remoteBranchName, tempRemote) {
					foundBranch = true
					break
				}
			}
			if !foundBranch {
				jirix.Logger.Errorf("Note: For project %q(%v), remote url has changed. Its branch %q is on a commit", op.project.Name, op.project.Path, branch.Name)
				jirix.Logger.Errorf("which is not in new remote(%v). Please manually reset your branches or move", op.project.Remote)
				jirix.Logger.Errorf("your project folder out of the root and try again")
				return nil
			}

		}
	}

	// Everything ok, change the remote url
	if err := git.SetRemoteUrl("origin", op.project.Remote); err != nil {
		return err
	}

	if err := fetch(jirix, op.project.Path, "", gitutil.AllOpt(true), gitutil.PruneOpt(true)); err != nil {
		return err
	}

	if err := syncProjectMaster(jirix, op.project, op.state, op.rebaseTracked, op.rebaseUntracked, op.rebaseAll, op.rebaseSubmodules, op.snapshot); err != nil {
		return err
	}

	return writeMetadata(jirix, op.project, op.project.Path)
}

func (op changeRemoteOperation) String() string {
	return fmt.Sprintf("Change remote of project %q to %q and update it to %q", op.project.Name, op.project.Remote, fmtRevision(op.project.Revision))
}

func (op changeRemoteOperation) Test(jirix *jiri.X) error {
	return nil
}

// updateOperation represents the update of a project.
type updateOperation struct {
	commonOperation
	rebaseTracked    bool
	rebaseUntracked  bool
	rebaseAll        bool
	rebaseSubmodules bool
	snapshot         bool
}

func (op updateOperation) Kind() string {
	return updateOpKind
}

func (op updateOperation) Run(jirix *jiri.X) error {
	if err := syncProjectMaster(jirix, op.project, op.state, op.rebaseTracked, op.rebaseUntracked, op.rebaseAll, op.rebaseSubmodules, op.snapshot); err != nil {
		return err
	}
	// If we enabled submodules and current project is a superproject, we need to remove initial branches and foo branch.
	if jirix.EnableSubmodules && op.project.GitSubmodules {
		if err := removeSubmoduleBranches(jirix, op.project, SubmoduleLocalFlagBranch); err != nil {
			return err
		}
	}
	return writeMetadata(jirix, op.project, op.project.Path)
}

func (op updateOperation) String() string {
	return fmt.Sprintf("advance/rebase project %q located in %q to %q", op.project.Name, op.source, fmtRevision(op.project.Revision))
}

func (op updateOperation) Test(jirix *jiri.X) error {
	return nil
}

// nullOperation represents a noop.  It is used for logging and adding project
// information to the current manifest.
type nullOperation struct {
	commonOperation
}

func (op nullOperation) Kind() string {
	return nullOpKind
}

func (op nullOperation) Run(jirix *jiri.X) error {
	return writeMetadata(jirix, op.project, op.project.Path)
}

func (op nullOperation) String() string {
	return fmt.Sprintf("project %q located in %q at revision %q is up-to-date", op.project.Name, op.source, fmtRevision(op.project.Revision))
}

func (op nullOperation) Test(jirix *jiri.X) error {
	return nil
}

// operations is a sortable collection of operations
type operations []operation

// Len returns the length of the collection.
func (ops operations) Len() int {
	return len(ops)
}

// Less defines the order of operations. Operations are ordered first
// by their type and then by their project path.
//
// The order in which operation types are defined determines the order
// in which operations are performed. For correctness and also to
// minimize the chance of a conflict, the delete operations should
// happen before change-remote operations, which should happen before move
// operations. If two create operations make nested directories, the
// outermost should be created first.
//
// When 2 operations have a parent/child relationship, we attempt to do the
// following:
// 1) If the child is moving further down the directory tree, we order it
// before the parent's update with the assumption the parent may expand into
// the child's current directory.
// 2) If the child is moving up the directory tree, we order it after the
// parent's update with the assumption the parent may be contracting to make
// space for the child.
// 3) If the child is being created, we follow the same logic as #2.
// 4) We sub order all the moves from outward moves to inward moves so the
// logic of #1 and #2 function as expected within the sort.
func (ops operations) Less(i, j int) bool {
	isSubdir := func(child, parent string) bool {
		return strings.HasPrefix(child, parent+string(filepath.Separator))
	}

	opKindToPriority := func(kind string) int {
		var priority int
		switch kind {
		case deleteOpKind:
			priority = 0
		case changeRemoteOpKind:
			priority = 1
		case moveOpKind:
			priority = 2
		case updateOpKind:
			priority = 3
		case createOpKind:
			priority = 4
		case nullOpKind:
			priority = 5
		}
		return priority
	}

	if ops[i].Kind() == moveOpKind {
		if ops[j].Kind() == updateOpKind {
			// Move is in a child project of Update
			if isSubdir(ops[i].Source(), ops[j].Source()) {
				// Move out
				if isSubdir(ops[i].Source(), ops[i].Destination()) {
					return false // Move happens after update
				}
			}
		}
		if ops[j].Kind() == createOpKind {
			// Create is the parent of the move destination
			if isSubdir(ops[i].Destination(), ops[j].Destination()) {
				return false // Move happens after create
			}
		}
		if ops[j].Kind() == moveOpKind {
			// Move out
			if isSubdir(ops[i].Destination(), ops[i].Source()) {
				return true
				// Move in
			} else if isSubdir(ops[i].Source(), ops[i].Destination()) {
				return false
			}
		}
	}

	if ops[i].Kind() == createOpKind {
		if ops[j].Kind() == moveOpKind {
			// Move out
			if isSubdir(ops[j].Destination(), ops[i].Destination()) {
				return true
			}
		}
		if ops[j].Kind() == updateOpKind {
			// Create in child
			if isSubdir(ops[i].Destination(), ops[j].Destination()) {
				return false
			}
		}
	}

	if ops[i].Kind() == updateOpKind {
		if ops[j].Kind() == moveOpKind || ops[j].Kind() == createOpKind {
			// Op in child
			if isSubdir(ops[j].Destination(), ops[i].Source()) {
				// Move out
				if ops[j].Kind() == moveOpKind && isSubdir(ops[j].Destination(), ops[j].Source()) {
					return false // Move out happens before update
				}
				return true
			}
		}
	}

	if ops[i].Kind() != ops[j].Kind() {
		return opKindToPriority(ops[i].Kind()) < opKindToPriority(ops[j].Kind())
	}

	if ops[i].Kind() == deleteOpKind {
		return ops[i].Source() > ops[j].Source()
	}

	return ops[i].Destination() < ops[j].Destination()
}

// Swap swaps two elements of the collection.
func (ops operations) Swap(i, j int) {
	ops[i], ops[j] = ops[j], ops[i]
}

// computeOperations inputs a set of projects to update and the set of
// current and new projects (as defined by contents of the local file
// system and manifest file respectively) and outputs a collection of
// operations that describe the actions needed to update the target
// projects.
// In the case of submodules, computeOperation will check for necessary
// deletions of jiri projects and initialize submodules in place of projects.
func computeOperations(
	jirix *jiri.X,
	localProjects,
	remoteProjects Projects,
	states map[ProjectKey]*ProjectState,
	rebaseTracked,
	rebaseUntracked,
	rebaseAll,
	rebaseSubmodules,
	snapshot bool,
	localManifestProjects []string,
) (operations, error) {
	result := operations{}
	allProjects := map[ProjectKey]Project{}
	for _, p := range localProjects {
		allProjects[p.Key()] = p
	}
	for _, p := range remoteProjects {
		allProjects[p.Key()] = p
	}
	// When we are switching submodules to projects, we deinit all of the current existing local submodules.
	if !jirix.EnableSubmodules && containLocalSubmodules(localProjects) {
		for _, project := range localProjects {
			if !project.GitSubmodules {
				continue
			}
			scm := gitutil.New(jirix, gitutil.RootDirOpt(project.Path))
			jirix.Logger.Debugf("De-initializing submodules in %s(%s)", project.Name, project.Path)
			if err := scm.SubmoduleDeinit(); err != nil {
				return nil, err
			}
		}
	}

	skipProjects, err := getProjectsToSkip(slices.Collect(maps.Values(allProjects)), localManifestProjects)
	if err != nil {
		return nil, err
	}

	for key := range allProjects {
		if skipProjects[key] {
			continue
		}
		var local, remote *Project
		var state *ProjectState
		if project, ok := localProjects[key]; ok {
			local = &project
		}
		if project, ok := remoteProjects[key]; ok {
			// update remote local config
			if local != nil {
				project.LocalConfig = local.LocalConfig
				remoteProjects[key] = project
			}
			remote = &project
		}
		if s, ok := states[key]; ok {
			state = s
		}
		result = append(result, computeOp(jirix, local, remote, state, rebaseTracked, rebaseUntracked, rebaseAll, rebaseSubmodules, snapshot))
	}
	sort.Sort(result)
	return result, nil
}

// Based on localManifestProjects, determine which projects to skip updating.
// A project will be skipped unless it is in localManifestProjects, or is a
// dependency of one of the localManifestProjects.
func getProjectsToSkip(allProjects []Project, localManifestProjects []string) (map[ProjectKey]bool, error) {
	skipProjects := make(map[ProjectKey]bool)
	if len(localManifestProjects) == 0 {
		return skipProjects, nil
	}

	projectsByName := make(map[string]Project)
	for _, proj := range allProjects {
		projectsByName[proj.Name] = proj
	}

	// Validate local manifest projects.
	for _, imp := range localManifestProjects {
		if _, ok := projectsByName[imp]; !ok {
			return nil, fmt.Errorf("Local manifest project %q doesn't exist.", imp)
		}
	}

	// For each project, skip it if it's not a local manifest project or a
	// dependency of a local manifest project.
	for _, proj := range allProjects {
		skip := true
		// Traverse up the import graph to see if `proj` is a dependency of a
		// local manifest project.
		for curr := proj.Name; curr != ""; curr = projectsByName[curr].ImportedBy {
			if slices.Contains(localManifestProjects, curr) {
				skip = false
				break
			}
		}
		if skip {
			skipProjects[proj.Key()] = true
		}
	}

	return skipProjects, nil
}

func computeOp(jirix *jiri.X, local, remote *Project, state *ProjectState, rebaseTracked, rebaseUntracked, rebaseAll, rebaseSubmodules, snapshot bool) operation {
	switch {
	case local == nil && remote != nil:
		return createOperation{commonOperation{
			destination: remote.Path,
			project:     *remote,
			source:      "",
		}}
	case local != nil && remote == nil:
		// When submodules are enabled, all submodules are removed from remote projects, so submodules from remote are nil.
		// We skip operations on submodules when we enabled submodules and rely on superproject updates.
		if jirix.EnableSubmodules && local.IsSubmodule {
			return nullOperation{commonOperation{
				project: *local,
				source:  local.Path,
				state:   *state,
			}}
		}
		return deleteOperation{commonOperation{
			destination: "",
			project:     *local,
			source:      local.Path,
		}}
	case local != nil && remote != nil:
		// When we are switching from submodules to projects, submodules are all removed and all projects need to be created new.
		if !jirix.EnableSubmodules && local.IsSubmodule {
			return createOperation{commonOperation{
				destination: remote.Path,
				project:     *remote,
				source:      "",
			}}
		}

		localBranchesNeedUpdating := false
		if !snapshot {
			cb := state.CurrentBranch
			if rebaseAll {
				for _, branch := range state.Branches {
					if branch.Tracking != nil {
						if branch.Revision != branch.Tracking.Revision {
							localBranchesNeedUpdating = true
							break
						}
					} else if rebaseUntracked && rebaseAll {
						// We put checks for untracked-branch updating in syncProjectMaster function
						localBranchesNeedUpdating = true
						break
					}
				}
			} else if cb.Name != "" && cb.Tracking != nil && cb.Revision != cb.Tracking.Revision {
				localBranchesNeedUpdating = true
			}
		}
		switch {
		case local.Remote != remote.Remote:
			return changeRemoteOperation{commonOperation{
				destination: remote.Path,
				project:     *remote,
				source:      local.Path,
				state:       *state,
			}, rebaseTracked, rebaseUntracked, rebaseAll, rebaseSubmodules, snapshot}
		case local.Path != remote.Path:
			if remote.Path == jirix.Root {
				return createOperation{commonOperation{
					destination: remote.Path,
					project:     *remote,
					source:      "",
				}}
			}
			// moveOperation also does an update, so we don't need to check the
			// revision here.
			return moveOperation{commonOperation{
				destination: remote.Path,
				project:     *remote,
				source:      local.Path,
				state:       *state,
			}, rebaseTracked, rebaseUntracked, rebaseAll, rebaseSubmodules, snapshot}
		// No need to update projects when current project exists as a submodule
		case jirix.EnableSubmodules && local.IsSubmodule:
			return nullOperation{commonOperation{
				destination: remote.Path,
				project:     *remote,
				source:      local.Path,
				state:       *state,
			}}
		case snapshot && local.Revision != remote.Revision:
			return updateOperation{commonOperation{
				destination: remote.Path,
				project:     *remote,
				source:      local.Path,
				state:       *state,
			}, rebaseTracked, rebaseUntracked, rebaseAll, rebaseSubmodules, snapshot}
		case jirix.EnableSubmodules && local.GitSubmodules:
			// Always update superproject when submodules are enabled.
			return updateOperation{commonOperation{
				destination: remote.Path,
				project:     *remote,
				source:      local.Path,
				state:       *state,
			}, rebaseTracked, rebaseUntracked, rebaseAll, rebaseSubmodules, snapshot}
		case localBranchesNeedUpdating || (state.CurrentBranch.Name == "" && local.Revision != remote.Revision):
			return updateOperation{commonOperation{
				destination: remote.Path,
				project:     *remote,
				source:      local.Path,
				state:       *state,
			}, rebaseTracked, rebaseUntracked, rebaseAll, rebaseSubmodules, snapshot}
		case state.CurrentBranch.Tracking == nil && local.Revision != remote.Revision:
			return updateOperation{commonOperation{
				destination: remote.Path,
				project:     *remote,
				source:      local.Path,
				state:       *state,
			}, rebaseTracked, rebaseUntracked, rebaseAll, rebaseSubmodules, snapshot}
		default:
			return nullOperation{commonOperation{
				destination: remote.Path,
				project:     *remote,
				source:      local.Path,
				state:       *state,
			}}
		}
	default:
		panic("jiri: computeOp called with nil local and remote")
	}
}

// This function creates worktree and runs create operation in parallel
func runCreateOperations(jirix *jiri.X, ops []createOperation) error {
	jirix.TimerPush("create operations")
	defer jirix.TimerPop()
	count := len(ops)
	if count == 0 {
		return nil
	}

	type workTree struct {
		// dir is the top level directory in which operations will be performed
		dir string
		// op is an ordered list of operations that must be performed serially,
		// affecting dir
		ops []operation
		// after contains a tree of work that must be performed after ops
		after map[string]*workTree
	}
	head := &workTree{
		dir:   "",
		ops:   []operation{},
		after: make(map[string]*workTree),
	}

	for _, op := range ops {

		node := head
		parts := strings.Split(op.Project().Path, string(filepath.Separator))
		// walk down the file path tree, creating any work tree nodes as required
		for _, part := range parts {
			if part == "" {
				continue
			}
			next, ok := node.after[part]
			if !ok {
				next = &workTree{
					dir:   part,
					ops:   []operation{},
					after: make(map[string]*workTree),
				}
				node.after[part] = next
			}
			node = next
		}
		node.ops = append(node.ops, op)
	}

	workQueue := make(chan *workTree, count)
	errs := make(chan error, count)
	var wg sync.WaitGroup
	processTree := func(tree *workTree) {
		defer wg.Done()
		for _, op := range tree.ops {
			logMsg := fmt.Sprintf("Creating project %q", op.Project().Name)
			task := jirix.Logger.AddTaskMsg("%s", logMsg)
			jirix.Logger.Debugf("%v", op)
			if err := op.Run(jirix); err != nil {
				task.Done()
				errs <- fmt.Errorf("%s: %s", logMsg, err)
				return
			}
			task.Done()
		}
		for _, v := range tree.after {
			wg.Add(1)
			workQueue <- v
		}
	}
	wg.Add(1)
	workQueue <- head
	for i := uint(0); i < jirix.Jobs; i++ {
		go func() {
			for tree := range workQueue {
				processTree(tree)
			}
		}()
	}
	wg.Wait()
	close(workQueue)
	close(errs)

	return errFromChannel(errs)
}

type PathTrie struct {
	current  string
	children map[string]*PathTrie
}

func NewPathTrie() *PathTrie {
	return &PathTrie{
		current:  "",
		children: make(map[string]*PathTrie),
	}
}

func (p *PathTrie) Contains(path string) bool {
	parts := strings.Split(path, string(filepath.Separator))
	node := p
	for _, part := range parts {
		if part == "" {
			continue
		}
		child, ok := node.children[part]
		if !ok {
			return false
		}
		node = child
	}
	return true
}

func (p *PathTrie) Insert(path string) {
	parts := strings.Split(path, string(filepath.Separator))
	node := p
	for _, part := range parts {
		if part == "" {
			continue
		}
		child, ok := node.children[part]
		if !ok {
			child = &PathTrie{
				current:  part,
				children: make(map[string]*PathTrie),
			}
			node.children[part] = child
		}
		node = child
	}
}

func runDeleteOperations(jirix *jiri.X, ops []deleteOperation, gc bool) error {
	jirix.TimerPush("delete operations")
	defer jirix.TimerPop()
	if len(ops) == 0 {
		return nil
	}
	notDeleted := NewPathTrie()
	if !gc {
		msg := fmt.Sprintf("%d project(s) is/are marked to be deleted. Run '%s' to delete them.", len(ops), jirix.Color.Yellow("jiri update -gc"))
		if jirix.Logger.LoggerLevel < log.DebugLevel {
			msg = fmt.Sprintf("%s\nOr run '%s' or '%s' to see the list of projects.", msg, jirix.Color.Yellow("jiri update -v"), jirix.Color.Yellow("jiri status -d"))
		}
		jirix.Logger.Warningf("%s\n\n", msg)
		if jirix.Logger.LoggerLevel >= log.DebugLevel {
			msg = "List of project(s) marked to be deleted:"
			for _, op := range ops {
				msg = fmt.Sprintf("%s\nName: %s, Path: '%s'", msg, jirix.Color.Yellow(op.project.Name), jirix.Color.Yellow(op.source))
			}
			jirix.Logger.Debugf("%s\n\n", msg)
		}
		return nil
	}
	for _, op := range ops {
		if notDeleted.Contains(op.Project().Path) {
			// not deleting project, add it to trie
			notDeleted.Insert(op.source)
			rmCommand := jirix.Color.Yellow("rm -rf %q", op.source)
			msg := fmt.Sprintf("Project %q won't be deleted because of its sub project(s)", op.project.Name)
			msg += fmt.Sprintf("\nIf you no longer need it, invoke '%s'\n\n", rmCommand)
			jirix.Logger.Warningf("%s", msg)
			continue
		}
		logMsg := fmt.Sprintf("Deleting project %q", op.Project().Name)
		task := jirix.Logger.AddTaskMsg("%s", logMsg)
		jirix.Logger.Debugf("%s", op)
		if err := op.Run(jirix); err != nil {
			task.Done()
			return fmt.Errorf("%s: %s", logMsg, err)
		}
		task.Done()
		if _, err := os.Stat(op.source); err == nil {
			// project not deleted, add it to trie
			notDeleted.Insert(op.source)
		} else if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("Checking if %q exists", op.source)
		}
	}
	return nil
}

func runMoveOperations(jirix *jiri.X, ops []moveOperation) error {
	jirix.TimerPush("move operations")
	defer jirix.TimerPop()
	parentSrcPath := ""
	parentDestPath := ""
	for _, op := range ops {
		if parentSrcPath != "" && strings.HasPrefix(op.source, parentSrcPath) {
			op.source = filepath.Join(parentDestPath, strings.Replace(op.source, parentSrcPath, "", 1))
		} else {
			parentSrcPath = op.source
			parentDestPath = op.destination
		}
		logMsg := fmt.Sprintf("Moving and updating project %q", op.Project().Name)
		task := jirix.Logger.AddTaskMsg("%s", logMsg)
		jirix.Logger.Debugf("%s", op)
		if err := op.Run(jirix); err != nil {
			task.Done()
			return fmt.Errorf("%s: %s", logMsg, err)
		}
		task.Done()
	}
	return nil
}

func runCommonOperations(jirix *jiri.X, ops operations, loglevel log.LogLevel) error {
	jirix.TimerPush("common operations")
	defer jirix.TimerPop()
	for _, op := range ops {
		logMsg := fmt.Sprintf("Updating project %q", op.Project().Name)
		task := jirix.Logger.AddTaskMsg("%s", logMsg)
		jirix.Logger.Logf(loglevel, "%s", op)
		if err := op.Run(jirix); err != nil {
			task.Done()
			return fmt.Errorf("%s: %s", logMsg, err)
		}
		task.Done()
	}
	return nil
}

func renameDir(jirix *jiri.X, src, dst string) error {
	// Parent directory permissions
	perm := os.FileMode(0755)
	swapDir := jirix.SwapDir()

	// Hash src path as swap dir name
	h := fnv.New32a()
	h.Write([]byte(src))
	tmp := filepath.Join(swapDir, fmt.Sprintf("%d", h.Sum32()))
	// Ensure .jiri_root/swap exists
	if err := os.MkdirAll(swapDir, perm); err != nil {
		return err
	}

	// Move src -> tmp
	if err := osutil.Rename(src, tmp); err != nil {
		return err
	}

	if err := removeEmptyParents(jirix, dst); err != nil {
		jirix.Logger.Tracef("Could not remove empty directories for %s", dst)
	}

	// Ensure the dst's parent exists, it may have
	// been within src
	parentDir := filepath.Dir(dst)
	if err := os.MkdirAll(parentDir, perm); err != nil {
		return err
	}

	// Move tmp -> dst
	if err := osutil.Rename(tmp, dst); err != nil {
		if err := osutil.Rename(tmp, src); err != nil {
			jirix.Logger.Errorf("Could not move %s to %s, original contents are in %s. Please complete the move manually", src, dst, tmp)
		}
		return err
	}
	return nil
}
