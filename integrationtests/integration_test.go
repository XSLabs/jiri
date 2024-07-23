// Copyright 2024 The Fuchsia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package integrationtests

import (
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"go.fuchsia.dev/jiri/project"
)

func TestSimpleProject(t *testing.T) {
	t.Parallel()

	remoteDir := t.TempDir()
	setupGitRepo(t, remoteDir, map[string]any{
		"manifest": project.Manifest{
			Projects: []project.Project{
				{
					Name:   "manifest",
					Path:   "manifest_dir",
					Remote: remoteDir,
				},
			},
		},
	})

	root := t.TempDir()
	jiri := newJiri(t, root)
	jiri("init", "-analytics-opt=false", root)
	jiri("import", "manifest", remoteDir)
	jiri("update")

	wantFiles := []string{
		"manifest_dir/manifest",
	}

	gotFiles := listDirRecursive(t, root)
	if diff := cmp.Diff(wantFiles, gotFiles); diff != "" {
		t.Errorf("Wrong directory contents after update (-want +got):\n%s", diff)
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
	jiri := newJiri(t, root)
	jiri("init", "-analytics-opt=false", "-enable-submodules=true", root)
	jiri("import", "manifest", remoteDir)
	jiri("update")

	// TODO: this is the bit that causes issues.
	runSubprocess(t, filepath.Join(root, "manifest_dir"), "git", "checkout", "main")

	// Commit a new file, bar.txt, to the submodule's upstream.
	writeFile(t, filepath.Join(submoduleRemoteDir, "new_file.txt"), []byte("bar"))
	runSubprocess(t, submoduleRemoteDir, "git", "add", ".")
	runSubprocess(t, submoduleRemoteDir, "git", "commit", "-m", "Add new_file.txt")

	// Sync the submodule to its upstream in the remote.
	runSubprocess(t, filepath.Join(remoteDir, "submodule"), "git", "pull")
	runSubprocess(t, remoteDir, "git", "commit", "-a", "-m", "Update submodule")

	// Do an update, which should pull in the submodule changes.
	jiri("update")

	wantFiles := []string{
		"manifest_dir/manifest",
		"manifest_dir/submodule/foo.txt",
		// TODO(https://fxbug.dev/290956668): This file should also exist after
		// an update.
		// "manifest_dir/submodule/new_file.txt",
	}

	gotFiles := listDirRecursive(t, root)
	if diff := cmp.Diff(wantFiles, gotFiles); diff != "" {
		t.Errorf("Wrong directory contents after update (-want +got):\n%s", diff)
	}
}
