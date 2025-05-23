// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package gitutil

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"go.fuchsia.dev/jiri"
	"go.fuchsia.dev/jiri/envvar"
)

type GitError struct {
	Root        string
	Args        []string
	Output      string
	ErrorOutput string
	err         error
}

func Error(output, errorOutput string, err error, root string, args ...string) GitError {
	return GitError{
		Root:        root,
		Args:        args,
		Output:      output,
		ErrorOutput: errorOutput,
		err:         err,
	}
}

func (ge GitError) Error() string {
	result := fmt.Sprintf("(%s) git ", ge.Root)
	result += strings.Join(ge.Args, " ")
	result += "' failed:\n"
	result += "stdout:\n"
	result += ge.Output + "\n"
	result += "stderr:\n"
	result += ge.ErrorOutput
	result += "\ncommand fail error: " + ge.err.Error()
	return result
}

type Git struct {
	jirix     *jiri.X
	opts      map[string]string
	rootDir   string
	userName  string
	userEmail string
}

type gitOpt interface {
	gitOpt()
}
type AuthorDateOpt string
type CommitterDateOpt string
type RootDirOpt string
type UserNameOpt string
type UserEmailOpt string

func (AuthorDateOpt) gitOpt()    {}
func (CommitterDateOpt) gitOpt() {}
func (RootDirOpt) gitOpt()       {}
func (UserNameOpt) gitOpt()      {}
func (UserEmailOpt) gitOpt()     {}

type Reference struct {
	Name     string
	Revision string
	IsHead   bool
}

type Branch struct {
	*Reference
	Tracking *Reference
}

type Revision string
type BranchName string

const (
	RemoteType = "remote"
	LocalType  = "local"
)

// New is the Git factory.
func New(jirix *jiri.X, opts ...gitOpt) *Git {
	rootDir := jirix.Cwd
	userName := ""
	userEmail := ""
	env := map[string]string{}
	for _, opt := range opts {
		switch typedOpt := opt.(type) {
		case AuthorDateOpt:
			env["GIT_AUTHOR_DATE"] = string(typedOpt)
		case CommitterDateOpt:
			env["GIT_COMMITTER_DATE"] = string(typedOpt)
		case RootDirOpt:
			rootDir = string(typedOpt)
		case UserNameOpt:
			userName = string(typedOpt)
		case UserEmailOpt:
			userEmail = string(typedOpt)
		}
	}
	return &Git{
		jirix:     jirix,
		opts:      env,
		rootDir:   rootDir,
		userName:  userName,
		userEmail: userEmail,
	}
}

// RootDir returns the root directory of the git repository.
func (g *Git) RootDir() string {
	return g.rootDir
}

// Add adds a file to staging.
func (g *Git) Add(file string) error {
	return g.run("add", file)
}

// Add adds a file to staging.
func (g *Git) AddUpdatedFiles() error {
	return g.run("add", "-u")
}

// AddRemote adds a new remote with the given name and path.
func (g *Git) AddRemote(name, path string) error {
	return g.run("remote", "add", name, path)
}

// AddOrReplacePartialRemote adds a new partial remote with given name and path.
// If the name already exists, it replaces the named remote with new path.
func (g *Git) AddOrReplacePartialRemote(name, path string) error {
	configKey := fmt.Sprintf("remote.%s.url", name)
	if err := g.Config(configKey, path); err != nil {
		return err
	}
	configKey = fmt.Sprintf("remote.%s.partialCloneFilter", name)
	if err := g.Config(configKey, "blob:none"); err != nil {
		return err
	}
	configKey = fmt.Sprintf("remote.%s.promisor", name)
	if err := g.Config(configKey, "true"); err != nil {
		return err
	}
	configKey = fmt.Sprintf("remote.%s.fetch", name)
	configVal := fmt.Sprintf("+refs/heads/*:refs/remotes/%s/*", name)
	if err := g.Config(configKey, configVal); err != nil {
		return err
	}
	return nil
}

// AddOrReplaceRemote adds a new remote with given name and path. If the name
// already exists, it replaces the named remote with new path.
func (g *Git) AddOrReplaceRemote(name, path string) error {
	configKey := fmt.Sprintf("remote.%s.url", name)
	if err := g.Config(configKey, path); err != nil {
		return err
	}
	configKey = fmt.Sprintf("remote.%s.fetch", name)
	configVal := fmt.Sprintf("+refs/heads/*:refs/remotes/%s/*", name)
	if err := g.Config(configKey, configVal); err != nil {
		return err
	}
	return nil
}

// AssumeUnchanged registers file contents unchanged in the working tree to the index.
// Or unset the unchanged flag if unchanged set as false.
func (g *Git) AssumeUnchanged(unchanged bool, dir string) error {
	if unchanged {
		return g.run("update-index", "--assume-unchanged", dir)
	}
	return g.run("update-index", "--no-assume-unchanged", dir)
}

// GetRemoteBranchesContaining returns a slice of the remote branches
// which contains the given commit
func (g *Git) GetRemoteBranchesContaining(commit string) ([]string, error) {
	branches, _, err := g.GetBranches("-r", "--contains", commit)
	return branches, err
}

// BranchesDiffer tests whether two branches have any changes between them.
func (g *Git) BranchesDiffer(branch1, branch2 string) (bool, error) {
	out, err := g.runOutput("--no-pager", "diff", "--name-only", branch1+".."+branch2)
	if err != nil {
		return false, err
	}
	// If output is empty, then there is no difference.
	if len(out) == 0 {
		return false, nil
	}
	// Otherwise there is a difference.
	return true, nil
}

// GetAllBranchesInfo returns information about all branches.
func (g *Git) GetAllBranchesInfo() ([]Branch, error) {
	branchesInfo, err := g.runOutput("for-each-ref", "--format", "%(refname:short):%(upstream:short):%(objectname):%(HEAD):%(upstream)", "refs/heads")
	if err != nil {
		return nil, err
	}
	var upstreamRefs []string
	var branches []Branch
	for _, branchInfo := range branchesInfo {
		s := strings.SplitN(branchInfo, ":", 5)
		branch := Branch{
			&Reference{
				Name:     s[0],
				Revision: s[2],
				IsHead:   s[3] == "*",
			},
			nil,
		}
		if s[1] != "" {
			upstreamRefs = append(upstreamRefs, s[4])
		}
		branches = append(branches, branch)
	}

	args := append([]string{"show-ref"}, upstreamRefs...)
	if refsInfo, err := g.runOutput(args...); err == nil {
		refs := map[string]string{}
		for _, info := range refsInfo {
			strs := strings.SplitN(info, " ", 2)
			refs[strs[1]] = strs[0]
		}
		for i, branchInfo := range branchesInfo {
			s := strings.SplitN(branchInfo, ":", 5)
			if s[1] != "" {
				branches[i].Tracking = &Reference{
					Name:     s[1],
					Revision: refs[s[4]],
				}
			}
		}
	}

	return branches, nil
}

// IsRevAvailable checks if a commit hash is available locally.
func (g *Git) IsRevAvailable(jirix *jiri.X, remote, rev string) bool {
	// If it wants HEAD, always fetch.
	if rev == "HEAD" {
		return false
	}
	// Ensure the revision is present.
	if jirix.UsePartialClone(remote) {
		currentRevision, err := g.CurrentRevision()
		if err != nil {
			jirix.Logger.Errorf("could not get current revision\n")
			return false
		}
		expectedRevision, err := g.CurrentRevisionForRef(rev)
		if err != nil {
			jirix.Logger.Errorf("could not get revision\n")
			return false
		}
		if currentRevision != expectedRevision {
			return false
		}
	}
	if err := g.run("cat-file", "-e", rev); err != nil {
		return false
	}
	return true
}

// Checkout checks out the given ref.
func (g *Git) Checkout(ref string, opts ...CheckoutOpt) error {
	args := []string{"checkout"}
	var force ForceOpt = false
	var detach DetachOpt = false
	var recurseSubmodules RecurseSubmodulesOpt = false
	var rebaseSubmodules RebaseSubmodulesOpt = false
	for _, opt := range opts {
		switch typedOpt := opt.(type) {
		case ForceOpt:
			force = typedOpt
		case DetachOpt:
			detach = typedOpt
		case RecurseSubmodulesOpt:
			recurseSubmodules = typedOpt
		case RebaseSubmodulesOpt:
			rebaseSubmodules = typedOpt
		}
	}
	if force {
		args = append(args, "-f")
	}
	if detach {
		args = append(args, "--detach")
	}

	args = append(args, ref)
	if err := g.run(args...); err != nil {
		return err
	}
	// Update all submodules, both current and new.
	// Un-initialized submodules are updated one by one.
	if recurseSubmodules {
		if err := g.SubmoduleUpdateAll(bool(rebaseSubmodules)); err != nil {
			return err
		}
	}
	return nil
}

// SubmoduleDeinit de-initiates all local submodules.
func (g *Git) SubmoduleDeinit() error {
	args := []string{"submodule", "deinit", "--all"}
	return g.run(args...)
}

// SubmodulePaths returns uninitialized submodules' paths.
func (g *Git) SubmodulePaths() ([]string, error) {
	submoduleStatus, _ := g.SubmoduleStatus()
	return SubmodulePathFromStatus(submoduleStatus)
}

// SubmoduleUpdate updates submodules for current branch.
func (g *Git) SubmoduleUpdate(opts ...SubmoduleUpdateOpt) error {
	args := []string{"submodule", "update"}
	for _, opt := range opts {
		switch typedOpt := opt.(type) {
		case RebaseSubmodulesOpt:
			if typedOpt {
				args = append(args, "--rebase")
			}
		case InitOpt:
			if typedOpt {
				args = append(args, "--init")
			}
		}
	}
	// TODO(iankaz): Use Jiri jobsFlag setting (or set submodule.fetchJobs on superproject init)
	// Number of parallel children to be used for fetching submodules.
	args = append(args, "--jobs=50")
	return g.run(args...)
}

// SubmoduleUpdateAll updates all submodules, including the ones that are not yet inited.
func (g *Git) SubmoduleUpdateAll(rebaseSubmodules bool) error {
	var multiErr error
	// Update submodules that are currently inited first.
	if err := g.SubmoduleUpdate(InitOpt(false), RebaseSubmodulesOpt(rebaseSubmodules)); err != nil {
		multiErr = errors.Join(multiErr, err)
	}
	// Update un-inited submodules one by one with path.
	if err := g.SubmoduleUpdateNew(); err != nil {
		multiErr = errors.Join(multiErr, err)
	}
	return multiErr
}

func (g *Git) SubmoduleUpdateNew() error {
	var multiErr error
	submPaths, err := g.SubmodulePaths()
	if err != nil {
		multiErr = errors.Join(multiErr, err)
		return multiErr
	}
	if err := g.SubmoduleInit(submPaths); err != nil {
		multiErr = errors.Join(multiErr, err)
		return multiErr
	}
	var wg sync.WaitGroup
	fetchLimit := make(chan struct{}, 50)
	for _, path := range submPaths {
		wg.Add(1)
		fetchLimit <- struct{}{}
		go func(path string) {
			defer func() { <-fetchLimit }()
			defer wg.Done()
			if err := g.SubmoduleUpdateModule(path); err != nil {
				multiErr = errors.Join(multiErr, err)
			}
		}(path)
	}
	wg.Wait()
	return multiErr
}

// SubmoduleUpdateModule updates specific module by relative path.
func (g *Git) SubmoduleUpdateModule(path string) error {
	args := []string{"submodule", "update", "--", path}
	return g.run(args...)
}

// SubmoduleInit inits submodules by paths.
func (g *Git) SubmoduleInit(submPaths []string) error {
	args := []string{"submodule", "init", "--"}
	for _, path := range submPaths {
		args = append(args, path)
	}
	return g.run(args...)
}

// Clone clones the given repository to the given local path.  If reference is
// not empty it uses the given path as a reference/shared repo.
func (g *Git) Clone(repo, path string, opts ...CloneOpt) error {
	args := []string{"clone"}
	for _, opt := range opts {
		switch typedOpt := opt.(type) {
		case BareOpt:
			if typedOpt {
				args = append(args, "--bare")
			}
		case ReferenceOpt:
			reference := string(typedOpt)
			if reference != "" {
				args = append(args, []string{"--reference-if-able", reference}...)
			}
		case SharedOpt:
			if typedOpt {
				args = append(args, []string{"--shared", "--local"}...)
			}
		case NoCheckoutOpt:
			if typedOpt {
				args = append(args, "--no-checkout")
			}
		case DepthOpt:
			if typedOpt > 0 {
				args = append(args, []string{"--depth", strconv.Itoa(int(typedOpt))}...)
			}
		case OmitBlobsOpt:
			if typedOpt {
				args = append(args, "--filter=blob:none")
			}
		case RecurseSubmodulesOpt:
			// TODO(iankaz): Add setting submodule.fetchJobs in git config to jiri init
			if typedOpt {
				args = append([]string{"--recurse-submodules", "--jobs=16"}, args...)
			}
		case DissociateOpt:
			if typedOpt {
				args = append(args, "--dissociate")
			}
		}
	}
	args = append(args, repo)
	args = append(args, path)
	return g.run(args...)
}

// CloneMirror clones the given repository using mirror flag.
func (g *Git) CloneMirror(repo, path string, depth int) error {
	args := []string{"clone", "--mirror"}
	if depth > 0 {
		args = append(args, []string{"--depth", strconv.Itoa(depth)}...)
	}
	args = append(args, []string{repo, path}...)
	return g.run(args...)
}

// CloneRecursive clones the given repository recursively to the given local path.
func (g *Git) CloneRecursive(repo, path string) error {
	return g.run("clone", "--recursive", repo, path)
}

// Commit commits all files in staging with an empty message.
func (g *Git) Commit() error {
	return g.run("commit", "--allow-empty", "--allow-empty-message", "--no-edit")
}

// CommitAmend amends the previous commit with the currently staged
// changes. Empty commits are allowed.
func (g *Git) CommitAmend() error {
	return g.run("commit", "--amend", "--allow-empty", "--no-edit")
}

// CommitAmendWithMessage amends the previous commit with the
// currently staged changes, and the given message. Empty commits are
// allowed.
func (g *Git) CommitAmendWithMessage(message string) error {
	return g.run("commit", "--amend", "--allow-empty", "-m", message)
}

// CommitAndEdit commits all files in staging and allows the user to
// edit the commit message.
func (g *Git) CommitAndEdit() error {
	args := []string{"commit", "--allow-empty"}
	return g.runInteractive(args...)
}

// CommitFile commits the given file with the given commit message.
func (g *Git) CommitFile(fileName, message string) error {
	if err := g.Add(fileName); err != nil {
		return err
	}
	return g.CommitWithMessage(message)
}

// CommitMessages returns the concatenation of all commit messages on
// <branch> that are not also on <baseBranch>.
func (g *Git) CommitMessages(branch, baseBranch string) (string, error) {
	out, err := g.runOutput("log", "--no-merges", baseBranch+".."+branch)
	if err != nil {
		return "", err
	}
	return strings.Join(out, "\n"), nil
}

// CommitNoVerify commits all files in staging with the given
// message and skips all git-hooks.
func (g *Git) CommitNoVerify(message string) error {
	return g.run("commit", "--allow-empty", "--allow-empty-message", "--no-verify", "-m", message)
}

// CommitWithMessage commits all files in staging with the given
// message.
func (g *Git) CommitWithMessage(message string) error {
	return g.run("commit", "--allow-empty", "--allow-empty-message", "-m", message)
}

// CommitWithMessage commits all files in staging and allows the user
// to edit the commit message. The given message will be used as the
// default.
func (g *Git) CommitWithMessageAndEdit(message string) error {
	args := []string{"commit", "--allow-empty", "-e", "-m", message}
	return g.runInteractive(args...)
}

// Committers returns a list of committers for the current repository
// along with the number of their commits.
func (g *Git) Committers() ([]string, error) {
	out, err := g.runOutput("shortlog", "-s", "-n", "-e")
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Provides list of commits reachable from rev but not from base
// rev can be a branch/tag or revision name.
func (g *Git) ExtraCommits(rev, base string) ([]string, error) {
	return g.runOutput("rev-list", base+".."+rev)
}

// CountCommits returns the number of commits on <branch> that are not
// on <base>.
func (g *Git) CountCommits(branch, base string) (int, error) {
	args := []string{"rev-list", "--count", branch}
	if base != "" {
		args = append(args, "^"+base)
	}
	args = append(args, "--")
	out, err := g.runOutput(args...)
	if err != nil {
		return 0, err
	}
	if got, want := len(out), 1; got != want {
		return 0, fmt.Errorf("unexpected length of %v: got %v, want %v", out, got, want)
	}
	count, err := strconv.Atoi(out[0])
	if err != nil {
		return 0, fmt.Errorf("Atoi(%v) failed: %v", out[0], err)
	}
	return count, nil
}

// Get one line log
func (g *Git) OneLineLog(rev string) (string, error) {
	out, err := g.runOutput("log", "--pretty=oneline", "-n", "1", "--abbrev-commit", rev)
	if err != nil {
		return "", err
	}
	if got, want := len(out), 1; got != want {
		g.jirix.Logger.Warningf("wanted one line log, got %d line log: %q", got, out)
	}
	return out[0], nil
}

// CheckBranchExists checks if a branch exists locally.
func (g *Git) CheckBranchExists(branch string) (bool, error) {
	out, err := g.runOutput("show-branch", branch)
	if len(out) == 0 {
		return false, err
	}
	return true, err
}

// CreateBranch creates a new branch with the given name.
func (g *Git) CreateBranch(branch string) error {
	return g.run("branch", branch)
}

// CreateBranchFromRef creates a new branch from an existing reference.
func (g *Git) CreateBranchFromRef(branch, ref string) error {
	return g.run("branch", branch, ref)
}

// CreateAndCheckoutBranch creates a new branch with the given name
// and checks it out.
func (g *Git) CreateAndCheckoutBranch(branch string) error {
	return g.run("checkout", "-b", branch)
}

// SetUpstream sets the upstream branch to the given one.
func (g *Git) SetUpstream(branch, upstream string) error {
	return g.run("branch", "-u", upstream, branch)
}

// LsRemote lists references in a remote repository.
func (g *Git) LsRemote(args ...string) (string, error) {
	a := []string{"ls-remote"}
	a = append(a, args...)
	out, err := g.runOutput(a...)
	if err != nil {
		return "", err
	}
	if got, want := len(out), 1; got != want {
		return "", fmt.Errorf("git ls-remote %s: unexpected length of %s: got %d, want %d", strings.Join(args, " "), out, got, want)
	}
	return out[0], nil
}

// CreateBranchWithUpstream creates a new branch and sets the upstream
// repository to the given upstream.
func (g *Git) CreateBranchWithUpstream(branch, upstream string) error {
	return g.run("branch", branch, upstream)
}

// ShortHash returns the short hash for a given reference.
func (g *Git) ShortHash(ref string) (string, error) {
	out, err := g.runOutput("rev-parse", "--short", ref)
	if err != nil {
		return "", err
	}
	if got, want := len(out), 1; got != want {
		return "", fmt.Errorf("unexpected length of %v: got %v, want %v", out, got, want)
	}
	return out[0], nil
}

// UserInfoForCommit returns user name and email for a given reference.
func (g *Git) UserInfoForCommit(ref string) (string, string, error) {
	out, err := g.runOutput("log", "-n", "1", "--format=format:%cn:%ce", ref)
	if err != nil {
		return "", "", err
	}
	info := strings.SplitN(out[0], ":", 2)
	return info[0], info[1], nil
}

// CurrentBranchName returns the name of the current branch.
func (g *Git) CurrentBranchName() (string, error) {
	out, err := g.runOutput("rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", err
	}
	if got, want := len(out), 1; got != want {
		return "", fmt.Errorf("unexpected length of %v: got %v, want %v", out, got, want)
	}
	return out[0], nil
}

func (g *Git) GetSymbolicRef() (string, error) {
	out, err := g.runOutput("symbolic-ref", "-q", "HEAD")
	if err != nil {
		return "", err
	}
	if got, want := len(out), 1; got != want {
		return "", fmt.Errorf("unexpected length of %v: got %v, want %v", out, got, want)
	}
	return out[0], nil
}

// RemoteBranchName returns the name of the tracking branch stripping remote name from it.
// It will search recursively if current branch tracks a local branch.
func (g *Git) RemoteBranchName() (string, error) {
	branch, err := g.CurrentBranchName()
	if err != nil || branch == "" {
		return "", err
	}

	trackingBranch, err := g.TrackingBranchName()
	if err != nil || trackingBranch == "" {
		return "", err
	}

	for {
		out, err := g.runOutput("config", "branch."+branch+".remote")
		if err != nil || len(out) == 0 {
			return "", err
		}
		if got, want := len(out), 1; got != want {
			return "", fmt.Errorf("unexpected length of %v: got %v, want %v", out, got, want)
		}
		// check if current branch tracks local branch
		if out[0] != "." {
			return strings.Replace(trackingBranch, out[0]+"/", "", 1), nil
		} else {
			branch = trackingBranch
			if trackingBranch, err = g.TrackingBranchFromSymbolicRef("refs/heads/" + trackingBranch); err != nil || trackingBranch == "" {
				return "", err
			}
		}
	}
}

// TrackingBranchName returns the name of the tracking branch.
func (g *Git) TrackingBranchName() (string, error) {
	currentRef, err := g.GetSymbolicRef()
	if err != nil {
		return "", err
	}
	return g.TrackingBranchFromSymbolicRef(currentRef)
}

// TrackingBranchFromSymbolicRef returns the name of the tracking branch for provided ref
func (g *Git) TrackingBranchFromSymbolicRef(ref string) (string, error) {
	out, err := g.runOutput("for-each-ref", "--format", "%(upstream:short)", ref)
	if err != nil || len(out) == 0 {
		return "", err
	}
	if got, want := len(out), 1; got != want {
		return "", fmt.Errorf("unexpected length of %v: got %v, want %v", out, got, want)
	}
	return out[0], nil
}

func (g *Git) IsOnBranch() bool {
	_, err := g.runOutput("symbolic-ref", "-q", "HEAD")
	return err == nil
}

// CurrentGitHooksPath returns the gitHooks directory of a project.
// Submodules gitHooks are under //.git/modules under superproject.
func (g *Git) CurrentGitHooksPath() (string, error) {
	out, err := g.runOutput("rev-parse", "--git-path", "hooks")
	if err != nil {
		return "", err
	}
	if len(out) != 1 {
		return "", fmt.Errorf("unexpected length of %v: got %v, want 1", out, len(out))
	}
	hooksPath := out[0]
	// rev-parse will return an absolute path if the hooks directory and the cwd
	// don't share a prefix. Only newer versions of git (2.31.0 and later)
	// support the `--path-format=absolute` flag to `git rev-parse`.
	if filepath.IsAbs(hooksPath) {
		return hooksPath, nil
	}
	return filepath.Join(g.rootDir, hooksPath), nil
}

// CurrentRevision returns the current revision.
func (g *Git) CurrentRevision() (string, error) {
	return g.CurrentRevisionForRef("HEAD")
}

// CurrentRevisionForRef gets current rev for ref/branch/tags
func (g *Git) CurrentRevisionForRef(ref string) (string, error) {
	if _, err := hex.DecodeString(ref); len(ref) == 40 && err == nil {
		// b/234618400 annotated tags create hash, which looks identical to commit hash.
		// cat-file command returns "tag" for annotated tags (note: returns commit for non-annotated tags)
		if out, err := g.runOutput("cat-file", "-t", ref); err == nil && len(out) == 1 && out[0] == "commit" {
			return ref, nil
		}
	}
	out, err := g.runOutput("rev-list", "-n", "1", ref)
	if err != nil {
		return "", err
	}
	if got, want := len(out), 1; got != want {
		return "", fmt.Errorf("unexpected length of %v: got %v, want %v", out, got, want)
	}
	return out[0], nil
}

// CurrentRevisionOfBranch returns the current revision of the given branch.
func (g *Git) CurrentRevisionOfBranch(branch string) (string, error) {
	// Using rev-list instead of rev-parse as latter doesn't work well with tag
	out, err := g.runOutput("rev-list", "-n", "1", branch)
	if err != nil {
		return "", err
	}
	if got, want := len(out), 1; got != want {
		return "", fmt.Errorf("unexpected length of %v: got %v, want %v", out, got, want)
	}
	return out[0], nil
}

func (g *Git) CherryPick(rev string) error {
	err := g.run("cherry-pick", rev)
	return err
}

// DeleteBranch deletes the given branch.
func (g *Git) DeleteBranch(branch string, opts ...DeleteBranchOpt) error {
	args := []string{"branch"}
	force := false
	for _, opt := range opts {
		switch typedOpt := opt.(type) {
		case ForceOpt:
			force = bool(typedOpt)
		}
	}
	if force {
		args = append(args, "-D")
	} else {
		args = append(args, "-d")
	}
	args = append(args, branch)
	return g.run(args...)
}

// DirExistsOnBranch returns true if a directory with the given name
// exists on the branch.  If branch is empty it defaults to "main".
func (g *Git) DirExistsOnBranch(dir, branch string) bool {
	if dir == "." {
		dir = ""
	}
	if branch == "" {
		branch = "main"
	}
	args := []string{"ls-tree", "-d", branch + ":" + dir}
	return g.run(args...) == nil
}

// CreateLightweightTag creates a lightweight tag with a given name.
func (g *Git) CreateLightweightTag(name string) error {
	return g.run("tag", name)
}

// Fetch fetches refs and tags from the given remote.
func (g *Git) Fetch(remote string, opts ...FetchOpt) error {
	return g.FetchRefspec(remote, "", opts...)
}

// FetchRefspec fetches refs and tags from the given remote for a particular refspec.
func (g *Git) FetchRefspec(remote, refspec string, opts ...FetchOpt) error {
	tags := false
	all := false
	prune := false
	updateShallow := false
	depth := 0
	fetchTag := ""
	updateHeadOk := false
	jobs := uint(0)
	recurseSubmodules := false
	for _, opt := range opts {
		switch typedOpt := opt.(type) {
		case TagsOpt:
			tags = bool(typedOpt)
		case AllOpt:
			all = bool(typedOpt)
		case PruneOpt:
			prune = bool(typedOpt)
		case DepthOpt:
			depth = int(typedOpt)
		case UpdateShallowOpt:
			updateShallow = bool(typedOpt)
		case FetchTagOpt:
			fetchTag = string(typedOpt)
		case UpdateHeadOkOpt:
			updateHeadOk = bool(typedOpt)
		case JobsOpt:
			jobs = uint(typedOpt)
		case RecurseSubmodulesOpt:
			recurseSubmodules = bool(typedOpt)
		}
	}
	args := []string{}
	args = append(args, "fetch")
	if recurseSubmodules {
		args = append(args, "--recurse-submodules")
	}
	if prune {
		args = append(args, "-p")
	}
	if tags {
		args = append(args, "--tags")
	}
	if depth > 0 {
		args = append(args, "--depth", strconv.Itoa(depth))
	}
	if updateShallow {
		args = append(args, "--update-shallow")
	}
	if all {
		args = append(args, "--all")
	}
	if updateHeadOk {
		args = append(args, "--update-head-ok")
	}
	if jobs > 0 {
		args = append(args, "--jobs="+strconv.FormatUint(uint64(jobs), 10))
	}
	if remote != "" {
		args = append(args, remote)
	}
	if fetchTag != "" {
		args = append(args, "tag", fetchTag)
	}
	if refspec != "" {
		args = append(args, refspec)
	}

	return g.run(args...)
}

// FilesWithUncommittedChanges returns the list of files that have
// uncommitted changes.
func (g *Git) FilesWithUncommittedChanges() ([]string, error) {
	out, err := g.runOutput("diff", "--name-only", "--no-ext-diff")
	if err != nil {
		return nil, err
	}
	out2, err := g.runOutput("diff", "--cached", "--name-only", "--no-ext-diff")
	if err != nil {
		return nil, err
	}
	return append(out, out2...), nil
}

// MergedBranches returns the list of all branches that were already merged.
func (g *Git) MergedBranches(ref string) ([]string, error) {
	branches, _, err := g.GetBranches("--merged", ref)
	return branches, err
}

// GetBranches returns a slice of the local branches of the current
// repository, followed by the name of the current branch. The
// behavior can be customized by providing optional arguments
// (e.g. --merged).
func (g *Git) GetBranches(args ...string) ([]string, string, error) {
	args = append([]string{"branch"}, args...)
	out, err := g.runOutput(args...)
	if err != nil {
		return nil, "", err
	}
	branches, current := []string{}, ""
	for _, branch := range out {
		if strings.HasPrefix(branch, "*") {
			branch = strings.TrimSpace(strings.TrimPrefix(branch, "*"))
			if g.IsOnBranch() {
				current = branch
			} else {
				// Do not append detached head
				continue
			}
		}
		branches = append(branches, strings.TrimSpace(branch))
	}
	return branches, current, nil
}

// BranchExists tests whether a branch with the given name exists in
// the local repository.
func (g *Git) BranchExists(branch string) (bool, error) {
	var stdout, stderr bytes.Buffer
	args := []string{"rev-parse", "--verify", "--quiet", branch}
	err := g.runGit(&stdout, &stderr, args...)
	if err != nil && stderr.String() != "" {
		return false, Error(stdout.String(), stderr.String(), err, g.rootDir, args...)
	}
	return stdout.String() != "", nil
}

// ListRemoteBranchesContainingRef returns a slice of the remote branches
// which contains the given commit
func (g *Git) ListRemoteBranchesContainingRef(commit string) (map[string]bool, error) {
	branches, _, err := g.GetBranches("-r", "--contains", commit)
	if err != nil {
		return nil, err
	}
	m := make(map[string]bool)
	for _, branch := range branches {
		m[branch] = true
	}
	return m, nil
}

// ListBranchesContainingRef returns a slice of the local branches
// which contains the given commit
func (g *Git) ListBranchesContainingRef(commit string) (map[string]bool, error) {
	branches, _, err := g.GetBranches("--contains", commit)
	if err != nil {
		return nil, err
	}
	m := make(map[string]bool)
	for _, branch := range branches {
		m[branch] = true
	}
	return m, nil
}

// Grep searches for matching text and returns a list of lines from
// `git grep`.
func (g *Git) Grep(query string, pathSpecs []string, flags ...string) ([]string, error) {
	args := append([]string{"grep"}, flags...)
	if query != "" {
		args = append(args, query)
	}
	if len(pathSpecs) != 0 {
		args = append(args, "--")
		args = append(args, pathSpecs...)
	}
	// TODO(ianloic): handle patterns that start with "-"
	// TODO(ianloic): handle different pattern types (-i, -P, -E, etc)
	// TODO(ianloic): handle different response types (--full-name, -v, --name-only, etc)
	return g.runOutput(args...)
}

// HasUncommittedChanges checks whether the current branch contains
// any uncommitted changes.
func (g *Git) HasUncommittedChanges() (bool, error) {
	out, err := g.FilesWithUncommittedChanges()
	if err != nil {
		return false, err
	}
	return len(out) != 0, nil
}

// HasUntrackedFiles checks whether the current branch contains any
// untracked files.
func (g *Git) HasUntrackedFiles() (bool, error) {
	out, err := g.UntrackedFiles()
	if err != nil {
		return false, err
	}
	return len(out) != 0, nil
}

// Init initializes a new git repository.
func (g *Git) Init(path string, opts ...CloneOpt) error {
	// TODO(fxb/65012): Add default branch detection in jiri.
	args := []string{"init"}
	major, minor, err := g.Version()
	if err != nil {
		return err
	}
	if major >= 2 && minor >= 28 {
		args = append(args, "--initial-branch=main")
	}
	for _, opt := range opts {
		switch typedOpt := opt.(type) {
		case BareOpt:
			if typedOpt {
				args = append(args, "--bare")
			}
		}
	}
	args = append(args, path)
	return g.run(args...)
}

// IsFileCommitted tests whether the given file has been committed to
// the repository.
func (g *Git) IsFileCommitted(file string) bool {
	// Check if file is still in staging environment.
	if out, _ := g.runOutput("status", "--porcelain", file); len(out) > 0 {
		return false
	}
	// Check if file is unknown to git.
	return g.run("ls-files", file, "--error-unmatch") == nil
}

func (g *Git) ShortStatus() (string, error) {
	out, err := g.runOutput("status", "-s")
	if err != nil {
		return "", err
	}
	return strings.Join(out, "\n"), nil
}

func (g *Git) CommitMsg(ref string) (string, error) {
	out, err := g.runOutput("log", "-n", "1", "--format=format:%B", ref)
	if err != nil {
		return "", err
	}
	return strings.Join(out, "\n"), nil
}

// LatestCommitMessage returns the latest commit message on the
// current branch.
func (g *Git) LatestCommitMessage() (string, error) {
	out, err := g.runOutput("log", "-n", "1", "--format=format:%B")
	if err != nil {
		return "", err
	}
	return strings.Join(out, "\n"), nil
}

// Log returns a list of commits on <branch> that are not on <base>,
// using the specified format.
func (g *Git) Log(branch, base, format string) ([][]string, error) {
	n, err := g.CountCommits(branch, base)
	if err != nil {
		return nil, err
	}
	result := [][]string{}
	for i := 0; i < n; i++ {
		skipArg := fmt.Sprintf("--skip=%d", i)
		formatArg := fmt.Sprintf("--format=%s", format)
		branchArg := fmt.Sprintf("%v..%v", base, branch)
		out, err := g.runOutput("log", "-1", skipArg, formatArg, branchArg)
		if err != nil {
			return nil, err
		}
		result = append(result, out)
	}
	return result, nil
}

// Merge merges all commits from <branch> to the current branch. If
// <squash> is set, then all merged commits are squashed into a single
// commit.
func (g *Git) Merge(branch string, opts ...MergeOpt) error {
	args := []string{"merge"}
	squash := false
	strategy := ""
	resetOnFailure := true
	for _, opt := range opts {
		switch typedOpt := opt.(type) {
		case SquashOpt:
			squash = bool(typedOpt)
		case StrategyOpt:
			strategy = string(typedOpt)
		case ResetOnFailureOpt:
			resetOnFailure = bool(typedOpt)
		case FfOnlyOpt:
			args = append(args, "--ff-only")
		}
	}
	if squash {
		args = append(args, "--squash")
	} else {
		args = append(args, "--no-squash")
	}
	if strategy != "" {
		args = append(args, fmt.Sprintf("--strategy=%v", strategy))
	}
	args = append(args, branch)
	if out, err := g.runOutput(args...); err != nil {
		if resetOnFailure {
			if err2 := g.run("reset", "--merge"); err2 != nil {
				return fmt.Errorf("%v\nCould not git reset while recovering from error: %v", err, err2)
			}
		}
		return fmt.Errorf("%v\n%v", err, strings.Join(out, "\n"))
	}
	return nil
}

// ModifiedFiles returns a slice of filenames that have changed
// between <baseBranch> and <currentBranch>.
func (g *Git) ModifiedFiles(baseBranch, currentBranch string) ([]string, error) {
	out, err := g.runOutput("diff", "--name-only", baseBranch+".."+currentBranch)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Pull pulls the given branch from the given remote.
func (g *Git) Pull(remote, branch string) error {
	if out, err := g.runOutput("pull", remote, branch); err != nil {
		g.run("reset", "--merge")
		return fmt.Errorf("%v\n%v", err, strings.Join(out, "\n"))
	}
	major, minor, err := g.Version()
	if err != nil {
		return err
	}
	// Starting with git 1.8, "git pull <remote> <branch>" does not
	// create the branch "<remote>/<branch>" locally. To avoid the need
	// to account for this, run "git pull", which fails but creates the
	// missing branch, for git 1.7 and older.
	if major < 2 && minor < 8 {
		// This command is expected to fail (with desirable side effects).
		// Use exec.Command instead of runner to prevent this failure from
		// showing up in the console and confusing people.
		if err := g.runInteractive("pull"); err != nil {
			return err
		}
	}
	return nil
}

// Push pushes the given branch to the given remote.
func (g *Git) Push(remote, branch string, opts ...PushOpt) error {
	args := []string{"push"}
	force := false
	verify := true
	// TODO(youngseokyoon): consider making followTags option default to true, after verifying that
	// it works well for the madb repository.
	followTags := false
	for _, opt := range opts {
		switch typedOpt := opt.(type) {
		case ForceOpt:
			force = bool(typedOpt)
		case VerifyOpt:
			verify = bool(typedOpt)
		case FollowTagsOpt:
			followTags = bool(typedOpt)
		}
	}
	if force {
		args = append(args, "--force")
	}
	if verify {
		args = append(args, "--verify")
	} else {
		args = append(args, "--no-verify")
	}
	if followTags {
		args = append(args, "--follow-tags")
	}
	args = append(args, remote, branch)
	return g.run(args...)
}

// RebaseBranch rebases the given branch on the given upstream.
func (g *Git) RebaseBranch(branch, upstream string, opts ...RebaseOpt) error {
	args := []string{"rebase", "--keep-empty"}
	rebaseMerges := false
	for _, opt := range opts {
		switch typedOpt := opt.(type) {
		case RebaseMerges:
			rebaseMerges = bool(typedOpt)
		}
	}

	if rebaseMerges {
		args = append(args, "--rebase-merges")
	}
	args = append(args, upstream)
	if branch != "" {
		args = append(args, branch)
	}
	return g.run(args...)
}

// Rebase rebases to a particular upstream branch.
func (g *Git) Rebase(upstream string, opts ...RebaseOpt) error {
	return g.RebaseBranch("", upstream, opts...)
}

// CherryPickAbort aborts an in-progress cherry-pick operation.
func (g *Git) CherryPickAbort() error {
	// First check if cherry-pick is in progress
	gitDir, err := g.AbsoluteGitDir()
	if err != nil {
		return err
	}
	path := "CHERRY_PICK_HEAD"
	if g.rootDir != "" {
		path = filepath.Join(gitDir, path)
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil // Not in progress return
		}
		return err
	}
	return g.run("cherry-pick", "--abort")
}

// RebaseAbort aborts an in-progress rebase operation. It should
// only be used after invoking Rebase().
func (g *Git) RebaseAbort() error {
	// First check if rebase is in progress
	out, err := g.runOutput("status", "--porcelain", "--untracked-files=no")
	if err != nil {
		return err
	}
	if len(out) == 0 {
		// rebase is not in progress
		return nil
	}
	// rebase is in progress, abort.
	return g.run("rebase", "--abort")
}

// Remove removes the given files.
func (g *Git) Remove(fileNames ...string) error {
	args := []string{"rm"}
	args = append(args, fileNames...)
	return g.run(args...)
}

func (g *Git) Config(configArgs ...string) error {
	args := []string{"config"}
	args = append(args, configArgs...)
	return g.run(args...)
}

func (g *Git) ConfigGetKey(key string) (string, error) {
	out, err := g.runOutput("config", "--get", key)
	if err != nil {
		return "", err
	}
	if got, want := len(out), 1; got != want {
		g.jirix.Logger.Warningf("wanted one line log, got %d line log: %q", got, out)
	}
	return out[0], nil
}

// RemoteUrl gets the url of the remote with the given name.
func (g *Git) RemoteUrl(name string) (string, error) {
	configKey := fmt.Sprintf("remote.%s.url", name)
	out, err := g.runOutput("config", "--get", configKey)
	if err != nil {
		return "", err
	}
	if got, want := len(out), 1; got != want {
		return "", fmt.Errorf("RemoteUrl: unexpected length of remotes %v: got %v, want %v", out, got, want)
	}
	return out[0], nil
}

// RemoveUntrackedFiles removes untracked files and directories.
func (g *Git) RemoveUntrackedFiles() error {
	return g.run("clean", "-d", "-f")
}

// Repack runs a `git repack` with the given options.
func (g *Git) Repack(opts ...RepackOpt) error {
	args := []string{"repack"}
	for _, opt := range opts {
		switch typedOpt := opt.(type) {
		case RepackAllOpt:
			if typedOpt {
				args = append(args, "-a")
			}
		case RemoveRedundantOpt:
			if typedOpt {
				args = append(args, "-d")
			}
		}
	}
	return g.run(args...)
}

// Reset resets the current branch to the target, discarding any
// uncommitted changes.
func (g *Git) Reset(target string, opts ...ResetOpt) error {
	args := []string{"reset"}
	mode := "hard"
	for _, opt := range opts {
		switch typedOpt := opt.(type) {
		case ModeOpt:
			mode = string(typedOpt)
		}
	}
	args = append(args, fmt.Sprintf("--%v", mode), target, "--")
	return g.run(args...)
}

// SetRemoteUrl sets the url of the remote with given name to the given url.
func (g *Git) SetRemoteUrl(name, url string) error {
	return g.run("remote", "set-url", name, url)
}

// SetRemoteHead sets the remote HEAD symref.
func (g *Git) SetRemoteHead() error {
	return g.run("remote", "set-head", "origin", "-a")
}

// SubmoduleConfig gets the field of the submodule from the submodule config.
func (g *Git) SubmoduleConfig(name, field string) (string, error) {
	configKey := fmt.Sprintf("submodule.%s.%s", name, field)
	out, err := g.runOutput("config", "--file", ".gitmodules", "--get", configKey)
	if err != nil {
		return "", err
	}
	if got := len(out); got != 1 {
		return "", fmt.Errorf("SubmoduleConfig: unexpected length of output field %v: got %v, want 1", out, got)
	}
	return out[0], nil
}

// DeleteRemote deletes the named remote
func (g *Git) DeleteRemote(name string) error {
	return g.run("remote", "rm", name)
}

// Stash attempts to stash any unsaved changes. It returns true if
// anything was actually stashed, otherwise false. An error is
// returned if the stash command fails.
func (g *Git) Stash() (bool, error) {
	oldSize, err := g.StashSize()
	if err != nil {
		return false, err
	}
	if err := g.run("stash", "save"); err != nil {
		return false, err
	}
	newSize, err := g.StashSize()
	if err != nil {
		return false, err
	}
	return newSize > oldSize, nil
}

// StashSize returns the size of the stash stack.
func (g *Git) StashSize() (int, error) {
	out, err := g.runOutput("stash", "list")
	if err != nil {
		return 0, err
	}
	// If output is empty, then stash is empty.
	if len(out) == 0 {
		return 0, nil
	}
	// Otherwise, stash size is the length of the output.
	return len(out), nil
}

// StashPop pops the stash into the current working tree.
func (g *Git) StashPop() error {
	return g.run("stash", "pop")
}

// SubmoduleStatus returns the status of the modules for under the superproject.
// If run under submodule directory, the directories of other submoudles will be
// relative to the submodule rootDir.
func (g *Git) SubmoduleStatus() ([]string, error) {
	out, err := g.runOutput("submodule", "status")
	if err != nil {
		return nil, err
	}
	return out, nil
}

// TopLevel returns the top level path of the current repository.
func (g *Git) TopLevel() (string, error) {
	// TODO(sadovsky): If g.rootDir is set, perhaps simply return that?
	out, err := g.runOutput("rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	return strings.Join(out, "\n"), nil
}

// TrackedFiles returns the list of files that are tracked.
func (g *Git) TrackedFiles() ([]string, error) {
	out, err := g.runOutput("ls-files")
	if err != nil {
		return nil, err
	}
	return out, nil
}

// IsInIndex returns if the directory is being tracked in git.
// If more than one projects are returned for the directory, return false.
func (g *Git) IsInIndex(dir string) bool {
	out, err := g.runOutput("ls-files", "--error-unmatch", "--", dir)
	if err != nil {
		return false
	}
	if len(out) != 1 {
		return false
	}
	return true
}

func (g *Git) Show(ref, file string) (string, error) {
	arg := ref
	arg = fmt.Sprintf("%s:%s", arg, file)
	out, err := g.runOutput("show", arg)
	if err != nil {
		return "", err
	}
	return strings.Join(out, "\n"), nil
}

// GitDir returns the absolute git dir for the repository.
func (g *Git) AbsoluteGitDir() (string, error) {
	out, err := g.runOutput("rev-parse", "--absolute-git-dir")
	if err != nil {
		return "", err
	}
	if got, want := len(out), 1; got != want {
		return "", fmt.Errorf("unexpected length of %v: got %v, want %v", out, got, want)
	}
	return out[0], nil
}

// UntrackedFiles returns the list of files that are not tracked.
func (g *Git) UntrackedFiles() ([]string, error) {
	out, err := g.runOutput("ls-files", "--others", "--directory", "--no-empty-directory", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Version returns the major and minor git version.
func (g *Git) Version() (int, int, error) {
	out, err := g.runOutput("version")
	if err != nil {
		return 0, 0, err
	}
	if got, want := len(out), 1; got != want {
		return 0, 0, fmt.Errorf("unexpected length of %v: got %v, want %v", out, got, want)
	}
	words := strings.Split(out[0], " ")
	if got, want := len(words), 3; got < want {
		return 0, 0, fmt.Errorf("unexpected length of %v: got %v, want at least %v", words, got, want)
	}
	version := strings.Split(words[2], ".")
	if got, want := len(version), 3; got < want {
		return 0, 0, fmt.Errorf("unexpected length of %v: got %v, want at least %v", version, got, want)
	}
	major, err := strconv.Atoi(version[0])
	if err != nil {
		return 0, 0, fmt.Errorf("failed parsing %q to integer", major)
	}
	minor, err := strconv.Atoi(version[1])
	if err != nil {
		return 0, 0, fmt.Errorf("failed parsing %q to integer", minor)
	}
	return major, minor, nil
}

func (g *Git) run(args ...string) error {
	var stdout, stderr bytes.Buffer
	if err := g.runGit(&stdout, &stderr, args...); err != nil {
		return Error(stdout.String(), stderr.String(), err, g.rootDir, args...)
	}
	return nil
}

func trimOutput(o string) []string {
	output := strings.TrimSpace(o)
	if len(output) == 0 {
		return nil
	}
	return strings.Split(output, "\n")
}

func (g *Git) runOutput(args ...string) ([]string, error) {
	var stdout, stderr bytes.Buffer
	if err := g.runGit(&stdout, &stderr, args...); err != nil {
		return nil, Error(stdout.String(), stderr.String(), err, g.rootDir, args...)
	}
	return trimOutput(stdout.String()), nil
}

func (g *Git) runInteractive(args ...string) error {
	var stderr bytes.Buffer
	// In order for the editing to work correctly with
	// terminal-based editors, notably "vim", use os.Stdout.
	if err := g.runGit(os.Stdout, &stderr, args...); err != nil {
		return Error("", stderr.String(), err, g.rootDir, args...)
	}
	return nil
}

func (g *Git) runGit(stdout, stderr io.Writer, args ...string) error {
	config := make(map[string]string)
	if g.userName != "" {
		config["user.name"] = g.userName
	}
	if g.userEmail != "" {
		config["user.email"] = g.userEmail
	}
	config["submodule.recurse"] = "false"
	if g.jirix.OffloadPackfiles {
		config["fetch.uriprotocols"] = "https"
	}
	// Allow add local directories as submodules, for testing purposes.
	config["protocol.file.allow"] = "always"

	var outbuf bytes.Buffer
	var errbuf bytes.Buffer
	command := exec.Command("git", args...)
	command.Dir = g.rootDir
	command.Stdin = os.Stdin
	command.Stdout = io.MultiWriter(stdout, &outbuf)
	command.Stderr = io.MultiWriter(stderr, &errbuf)
	env := g.jirix.Env()
	env = envvar.MergeMaps(g.opts, env, GitConfigEnvVars(config))
	// Disable git's advice notices that suggest trying different operations.
	// Such information isn't useful to show to users, since users aren't
	// running the commands directly.
	env["GIT_ADVICE"] = "0"
	command.Env = envvar.MapToSlice(env)
	dir := g.rootDir
	g.jirix.Logger.Tracef("Run: git %s (%s)", strings.Join(args, " "), dir)
	err := command.Run()
	exitCode := 0
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			exitCode = exitError.ExitCode()
		}
	}
	g.jirix.Logger.Tracef("Finished: git %s (%s), \nstdout: %s\nstderr: %s\nexit code: %v\n", strings.Join(args, " "), dir, outbuf.String(), errbuf.String(), exitCode)
	return err
}

// GitConfigEnvVars converts a git config key-value mapping into corresponding
// environment variables to pass to git.
//
// See https://git-scm.com/docs/git-config#ENVIRONMENT
func GitConfigEnvVars(config map[string]string) map[string]string {
	var keys []string
	for k := range config {
		keys = append(keys, k)
	}
	// Sort to ensure determinism.
	sort.Strings(keys)

	env := map[string]string{
		"GIT_CONFIG_COUNT": fmt.Sprintf("%d", len(keys)),
	}
	for i, k := range keys {
		env[fmt.Sprintf("GIT_CONFIG_KEY_%d", i)] = k
		env[fmt.Sprintf("GIT_CONFIG_VALUE_%d", i)] = config[k]
	}
	return env
}

// Committer encapsulates the process of create a commit.
type Committer struct {
	commit            func() error
	commitWithMessage func(message string) error
}

// Commit creates a commit.
func (c *Committer) Commit(message string) error {
	if len(message) == 0 {
		// No commit message supplied, let git supply one.
		return c.commit()
	}
	return c.commitWithMessage(message)
}

// NewCommitter is the Committer factory. The boolean <edit> flag
// determines whether the commit commands should prompt users to edit
// the commit message. This flag enables automated testing.
func (g *Git) NewCommitter(edit bool) *Committer {
	if edit {
		return &Committer{
			commit:            g.CommitAndEdit,
			commitWithMessage: g.CommitWithMessageAndEdit,
		}
	} else {
		return &Committer{
			commit:            g.Commit,
			commitWithMessage: g.CommitWithMessage,
		}
	}
}
