// Copyright 2024 The Fuchsia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package integrationtests

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"go.fuchsia.dev/jiri/project"
)

func TestUpdateWithSubmodule(t *testing.T) {
	t.Parallel()

	remoteDir := t.TempDir()
	setupGitRepo(t, remoteDir, map[string]any{
		"manifest": project.Manifest{
			Projects: []project.Project{
				{
					Name:          "manifest",
					Path:          "manifest_dir",
					Remote:        remoteDir,
					GitSubmodules: true,
				},
			},
		},
	})

	submoduleRemoteDir := t.TempDir()
	setupGitRepo(t, submoduleRemoteDir, map[string]any{
		"foo.txt": "foo",
	})

	runSubprocess(t, remoteDir, "git", "submodule", "add", submoduleRemoteDir, "submodule")
	runSubprocess(t, remoteDir, "git", "commit", "-a", "-m", "Add submodule")

	root := t.TempDir()
	jiri := jiriInit(t, root, "-enable-submodules=true")
	jiri("import", "manifest", remoteDir)
	jiri("update")

	submoduleLocalDir := filepath.Join(root, "manifest_dir", "submodule")
	submoduleHooksDir := strings.TrimSpace(runSubprocess(t, submoduleLocalDir,
		"git", "rev-parse", "--git-path", "hooks"))
	commitMsgHook := filepath.Join(submoduleHooksDir, "commit-msg")

	// TODO(olivernewman): The first `jiri update` isn't sufficient to install
	// git hooks for submodules. This is a bug.
	if fileExists(t, commitMsgHook) {
		t.Errorf("Bug is fixed, submodules git hooks are now installed by an initial jiri update")
	}

	wantJiriHead := runSubprocess(t, submoduleRemoteDir, "git", "rev-parse", "HEAD")

	// Make sure that Jiri sets the JIRI_HEAD ref even for submodules.
	gotJiriHead := runSubprocess(t, submoduleLocalDir, "git", "rev-parse", "JIRI_HEAD")
	if wantJiriHead != gotJiriHead {
		t.Errorf("JIRI_HEAD of submodule points to wrong commit %q, expected %q", gotJiriHead, wantJiriHead)
	}

	checkDirContents(t, root, []string{
		"manifest_dir/manifest",
		"manifest_dir/submodule/foo.txt",
	})

	// TODO(olivernewman): It's necessary to run `jiri update` twice in order to
	// get it to install git hooks for submodules. This is a bug.
	jiri("update")

	if !fileExists(t, commitMsgHook) {
		t.Errorf("commit-msg hook was not created for submodule")
	}
}

// Test that Jiri correctly updates submodules even when the superproject is
// checked out on a branch, versus a detached HEAD.
//
// Reproduces https://fxbug.dev/290956668.
func TestUpdateWithSubmodulesOnBranch(t *testing.T) {
	t.Parallel()

	remoteDir := t.TempDir()
	setupGitRepo(t, remoteDir, map[string]any{
		"manifest": project.Manifest{
			Projects: []project.Project{
				{
					Name:          "manifest",
					Path:          "manifest_dir",
					Remote:        remoteDir,
					GitSubmodules: true,
				},
			},
		},
	})

	submoduleRemoteDir := t.TempDir()
	setupGitRepo(t, submoduleRemoteDir, map[string]any{
		"foo.txt": "foo",
	})

	runSubprocess(t, remoteDir, "git", "submodule", "add", submoduleRemoteDir, "submodule")
	runSubprocess(t, remoteDir, "git", "commit", "-m", "Add submodule")

	root := t.TempDir()
	jiri := jiriInit(t, root, "-enable-submodules=true")
	jiri("import", "manifest", remoteDir)
	jiri("update")

	// Check out a branch. Jiri should still update submodules even when on the
	// branch.
	runSubprocess(t, filepath.Join(root, "manifest_dir"), "git", "checkout", "main")

	// Commit a new file to the submodule's upstream.
	writeFile(t, filepath.Join(submoduleRemoteDir, "new_file.txt"), "bar")
	runSubprocess(t, submoduleRemoteDir, "git", "add", ".")
	runSubprocess(t, submoduleRemoteDir, "git", "commit", "-m", "Add new_file.txt")

	// Sync the submodule to its upstream in the remote.
	runSubprocess(t, filepath.Join(remoteDir, "submodule"), "git", "pull")
	runSubprocess(t, remoteDir, "git", "commit", "-a", "-m", "Update submodule")

	// Do an update, which should pull in the submodule changes.
	jiri("update")

	checkDirContents(t, root, []string{
		"manifest_dir/manifest",
		"manifest_dir/submodule/foo.txt",
		// TODO(https://fxbug.dev/290956668): This file should also exist after
		// an update.
		// "manifest_dir/submodule/new_file.txt",
	})
}

func TestUpdateAfterLocalChangeToSubmodule(t *testing.T) {
	t.Parallel()

	remoteDir := t.TempDir()
	setupGitRepo(t, remoteDir, map[string]any{
		"manifest": project.Manifest{
			Projects: []project.Project{
				{
					Name:          "manifest",
					Path:          "manifest_dir",
					Remote:        remoteDir,
					GitSubmodules: true,
				},
			},
		},
	})

	submoduleRemoteDir := t.TempDir()
	setupGitRepo(t, submoduleRemoteDir, map[string]any{
		"foo.txt": "foo",
	})

	runSubprocess(t, remoteDir, "git", "submodule", "add", submoduleRemoteDir, "submodule")
	// Mimic the current behavior of Jiri in fuchsia.git, where all git
	// submodules have `ignore = all` set in .gitmodules.
	runSubprocess(t, remoteDir,
		"git", "config", "--file", ".gitmodules", "submodule.submodule.ignore", "all")
	runSubprocess(t, remoteDir, "git", "commit", "-a", "-m", "Add submodule")

	root := t.TempDir()
	jiri := jiriInit(t, root, "-enable-submodules=true")
	jiri("import", "manifest", remoteDir)
	jiri("update")

	// Commit a new file to the local copy of the submodule.
	submoduleLocalDir := filepath.Join(root, "manifest_dir", "submodule")
	writeFile(t, filepath.Join(submoduleLocalDir, "new_file.txt"), "bar")
	runSubprocess(t, submoduleLocalDir, "git", "add", ".")
	runSubprocess(t, submoduleLocalDir, "git", "commit", "-m", "Add new_file.txt")

	jiri("update")

	checkDirContents(t, root, []string{
		"manifest_dir/manifest",
		"manifest_dir/submodule/foo.txt",
		// TODO(https://fxbug.dev/290956668): This file should still exist after
		// an update - jiri should not update submodules that are not on
		// JIRI_HEAD.
		// "manifest_dir/submodule/new_file.txt",
	})
}

// Check that it's safe to toggle from -enable-submodules=true to -enable-submodules=false.
func TestDisablingSubmodules(t *testing.T) {
	t.Parallel()

	remoteDir := t.TempDir()
	submoduleRemoteDir := t.TempDir()
	setupGitRepo(t, remoteDir, map[string]any{
		"manifest": project.Manifest{
			Projects: []project.Project{
				{
					Name:          "manifest",
					Path:          "manifest_dir",
					Remote:        remoteDir,
					GitSubmodules: true,
				},
				{
					Name:           "submodule",
					Path:           "manifest_dir/submodule",
					Remote:         submoduleRemoteDir,
					GitSubmoduleOf: "manifest",
				},
			},
		},
	})

	setupGitRepo(t, submoduleRemoteDir, map[string]any{
		"foo.txt": "foo",
	})

	runSubprocess(t, remoteDir, "git", "submodule", "add", submoduleRemoteDir, "submodule")
	// Set `ignore = all` to mimic fuchsia.git's submodules setup.
	runSubprocess(t, remoteDir,
		"git", "config", "--file", ".gitmodules", "submodule.submodule.ignore", "all")
	runSubprocess(t, remoteDir, "git", "commit", "-a", "-m", "Add submodule")

	root := t.TempDir()
	jiri := jiriInit(t, root, "-enable-submodules=true")
	jiri("import", "manifest", remoteDir)
	jiri("update")

	manifestLocalDir := filepath.Join(root, "manifest_dir")

	if diff := cmp.Diff(
		fmt.Sprintf(" %s submodule (heads/main)\n", currentRevision(t, submoduleRemoteDir)),
		runSubprocess(t, manifestLocalDir, "git", "submodule", "status"),
	); diff != "" {
		t.Errorf("`git submodule status` diff (-want +got):\n%s", diff)
	}

	jiri("init", "-enable-submodules=false")
	jiri("update")

	checkDirContents(t, root, []string{
		"manifest_dir/manifest",
		"manifest_dir/submodule/foo.txt",
	})

	// The submodule should be de-initialized after disabling submodules and
	// running `jiri update`. `git submodule status` prints a hyphen at the
	// start of the line for any uninitialized submodules.
	if diff := cmp.Diff(
		fmt.Sprintf("-%s submodule\n", currentRevision(t, submoduleRemoteDir)),
		runSubprocess(t, manifestLocalDir, "git", "submodule", "status"),
	); diff != "" {
		t.Errorf("`git submodule status` diff (-want +got):\n%s", diff)
	}
}
