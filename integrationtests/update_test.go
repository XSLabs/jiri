// Copyright 2024 The Fuchsia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package integrationtests

import (
	"path/filepath"
	"strings"
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
	jiri := jiriInit(t, root)
	jiri("import", "manifest", remoteDir)
	jiri("update")

	checkDirContents(t, root, []string{
		"manifest_dir/manifest",
	})
}

func TestUpdateWithDirtyProject(t *testing.T) {
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
		"foo.txt": "original contents\n",
	})

	root := t.TempDir()
	jiri := jiriInit(t, root)
	jiri("import", "manifest", remoteDir)
	jiri("update")

	fooPath := filepath.Join(root, "manifest_dir", "foo.txt")
	newContents := "new contents\n"
	writeFile(t, fooPath, newContents)

	// A Jiri update should not discard uncommitted changes.
	jiri("update")

	got := readFile(t, fooPath)

	if diff := cmp.Diff(newContents, got); diff != "" {
		t.Errorf("Wrong foo.txt contents after update (-want +got):\n%s", diff)
	}
}

func TestImportRemoteManifest(t *testing.T) {
	t.Parallel()

	// Set up a remote repo containing a manifest that includes the repo itself
	// at the root.
	bRemoteDir := t.TempDir()
	setupGitRepo(t, bRemoteDir, map[string]any{
		"b_manifest": project.Manifest{
			Projects: []project.Project{
				// The project itself.
				{
					Name:   "b",
					Remote: bRemoteDir,
					Path:   ".",
				},
			},
		},
		"foo.txt": "foo\n",
	})

	// Set up a remote repo that imports the above repository at the root of the
	// checkout.
	aRemoteDir := t.TempDir()
	setupGitRepo(t, aRemoteDir, map[string]any{
		"a_manifest": project.Manifest{
			Imports: []project.Import{
				{
					Name:     "b",
					Manifest: "b_manifest",
					Remote:   bRemoteDir,
					Revision: currentRevision(t, bRemoteDir),
					Root:     "",
				},
			},
			Projects: []project.Project{
				{
					Name:   "a",
					Remote: aRemoteDir,
					Path:   "a",
				},
			},
		},
	})

	root := t.TempDir()
	jiri := jiriInit(t, root)
	jiri("import", "-name", "a", "a_manifest", aRemoteDir)
	jiri("update")

	checkDirContents(t, root, []string{
		"foo.txt",
		"b_manifest",
		"a/a_manifest",
	})
}

// Checks that -local-manifest works.
func TestUpdateWithLocalManifestChange(t *testing.T) {
	t.Parallel()

	subprojectRemoteDir := t.TempDir()
	setupGitRepo(t, subprojectRemoteDir, map[string]any{
		"foo.txt": "foo",
	})

	remoteDir := t.TempDir()
	setupGitRepo(t, remoteDir, map[string]any{
		"manifest": project.Manifest{
			Projects: []project.Project{
				{
					Name:   "manifest",
					Path:   "manifest_dir",
					Remote: remoteDir,
				},
				{
					Name:     "subproject",
					Path:     "subproject",
					Remote:   subprojectRemoteDir,
					Revision: currentRevision(t, subprojectRemoteDir),
				},
			},
		},
	})

	root := t.TempDir()
	jiri := jiriInit(t, root)
	jiri("import", "manifest", remoteDir)
	jiri("update")

	originalContents := []string{
		"manifest_dir/manifest",
		"subproject/foo.txt",
	}
	checkDirContents(t, root, originalContents)

	// Advance the subproject to a new revision, with a new file.
	writeFile(t, filepath.Join(subprojectRemoteDir, "new_file.txt"), "new file contents")
	runSubprocess(t, subprojectRemoteDir, "git", "add", "new_file.txt")
	runSubprocess(t, subprojectRemoteDir, "git", "commit", "-m", "Add new_file.txt")
	subprojectNewRevision := currentRevision(t, subprojectRemoteDir)

	// Edit the manifest to point to the new revision.
	localManifestPath := filepath.Join(root, "manifest_dir", "manifest")
	jiri("edit", "-project", "subproject="+subprojectNewRevision, localManifestPath)

	// A plain old `jiri update` should ignore the manifest changes...
	jiri("update")
	checkDirContents(t, root, originalContents)

	// ... but with the -local-manifest flag, Jiri should respect local manifest
	// changes and pull in the locally pinned version of the subproject, with
	// the new file.
	jiri("update", "-local-manifest")
	checkDirContents(t, root, append(originalContents,
		"subproject/new_file.txt",
	))
}

// Checks that -local-manifest-project <list of projects> respects changes in a manifest in an <import>ed
// project, not just the top-level manifest project.
func TestUpdateWithLocalManifestChangeInImportedProject(t *testing.T) {
	t.Parallel()

	cRemoteDir := t.TempDir()
	setupGitRepo(t, cRemoteDir, map[string]any{
		"foo.txt": "foo\n",
	})

	bRemoteDir := t.TempDir()
	setupGitRepo(t, bRemoteDir, map[string]any{
		"b_manifest": project.Manifest{
			Projects: []project.Project{
				{
					Name:   "b",
					Path:   ".",
					Remote: bRemoteDir,
				},
				{
					Name:     "c",
					Path:     "c",
					Remote:   cRemoteDir,
					Revision: currentRevision(t, cRemoteDir),
				},
			},
		},
	})

	// Set up a remote repo that imports the above repository at the root of the
	// checkout.
	remoteDir := t.TempDir()
	setupGitRepo(t, remoteDir, map[string]any{
		"a_manifest": project.Manifest{
			Imports: []project.Import{
				{
					Name:     "b",
					Manifest: "b_manifest",
					Remote:   bRemoteDir,
					Revision: currentRevision(t, bRemoteDir),
					Root:     "",
				},
			},
			Projects: []project.Project{
				{
					Name:   "a",
					Remote: remoteDir,
					Path:   "a",
				},
			},
		},
	})

	root := t.TempDir()
	jiri := jiriInit(t, root)
	jiri("import", "-name", "a", "a_manifest", remoteDir)
	jiri("update")

	checkDirContents(t, root, []string{
		"a/a_manifest",
		"b_manifest",
		"c/foo.txt",
	})

	// Create a new commit in the transitive dependency.
	writeFile(t, filepath.Join(cRemoteDir, "new_file.txt"), "contents")
	runSubprocess(t, cRemoteDir, "git", "add", "new_file.txt")
	runSubprocess(t, cRemoteDir, "git", "commit", "-m", "Add new_file.txt")
	newTransitiveDepRevision := currentRevision(t, cRemoteDir)

	// Edit the local manifest to point to the new revision.
	localManifestPath := filepath.Join(root, "b_manifest")
	jiri("edit", "-project", "c="+newTransitiveDepRevision, localManifestPath)

	jiri("update", "-local-manifest-project", "b")
	checkDirContents(t, root, []string{
		"a/a_manifest",
		"b_manifest",
		"c/foo.txt",
		"c/new_file.txt",
	})
}

// Checks that -local-manifest-project <list of projects> doesn't update a
// project that isn't in the <list of projects> passed to
// -local-manifest-project.
func TestUpdateWithProjectNotListedInLocalManifestProjects(t *testing.T) {
	t.Parallel()

	cRemoteDir := t.TempDir()
	setupGitRepo(t, cRemoteDir, map[string]any{
		"foo.txt": "foo\n",
	})

	bRemoteDir := t.TempDir()
	setupGitRepo(t, bRemoteDir, map[string]any{
		"b_manifest": project.Manifest{
			Projects: []project.Project{
				{
					Name:   "b",
					Path:   "b",
					Remote: bRemoteDir,
				},
				{
					Name:     "c",
					Path:     "c",
					Remote:   cRemoteDir,
					Revision: currentRevision(t, cRemoteDir),
				},
			},
		},
	})

	// Set up a remote repo that imports the above repository at the root of the
	// checkout.
	aRemoteDir := t.TempDir()
	setupGitRepo(t, aRemoteDir, map[string]any{
		"a_manifest": project.Manifest{
			Imports: []project.Import{
				{
					Name:     "b",
					Manifest: "b_manifest",
					Remote:   bRemoteDir,
					Revision: currentRevision(t, bRemoteDir),
					Root:     "",
				},
			},
			Projects: []project.Project{
				{
					Name:   "a",
					Remote: aRemoteDir,
					Path:   "a",
				},
			},
		},
	})

	root := t.TempDir()
	jiri := jiriInit(t, root)
	jiri("import", "-name", "a", "a_manifest", aRemoteDir)
	jiri("update")

	checkDirContents(t, root, []string{
		"a/a_manifest",
		"b/b_manifest",
		"c/foo.txt",
	})

	// Create a new commit in the transitive dependency.
	writeFile(t, filepath.Join(cRemoteDir, "new_file.txt"), "contents")
	runSubprocess(t, cRemoteDir, "git", "add", "new_file.txt")
	runSubprocess(t, cRemoteDir, "git", "commit", "-m", "Add new_file.txt")
	cNewRevision := currentRevision(t, cRemoteDir)

	// Edit the local manifest to point to the new revision.
	bManifestLocalPath := filepath.Join(root, "b", "b_manifest")
	jiri("edit", "-project", "c="+cNewRevision, bManifestLocalPath)

	jiri("update", "-local-manifest-project", "b")

	// Because -local-manifest-project is not specified, the manifest edit should not
	// take effect after this jiri update.
	jiri("update")
	checkDirContents(t, root, []string{
		"a/a_manifest",
		"b/b_manifest",
		"c/foo.txt",
	})
}

// Checks that if bar is an import of foo and `jiri update -local-manifest-project=bar` and `jiri
// update -local-manifest-project=foo local-manifest-project=bar` behave the same way: only bar
// is updated.
func TestUpdateOnlyImport(t *testing.T) {
	t.Parallel()

	bRemoteDir := t.TempDir()
	setupGitRepo(t, bRemoteDir, map[string]any{
		"b_manifest": project.Manifest{
			Projects: []project.Project{
				{
					Name:   "b",
					Path:   "b",
					Remote: bRemoteDir,
				},
			},
		},
	})

	// Set up a remote repo that imports the above repository.
	aRemoteDir := t.TempDir()
	setupGitRepo(t, aRemoteDir, map[string]any{
		"a_manifest": project.Manifest{
			Imports: []project.Import{
				{
					Name:     "b",
					Manifest: "b_manifest",
					Remote:   bRemoteDir,
					Revision: currentRevision(t, bRemoteDir),
					Root:     "",
				},
			},
			Projects: []project.Project{
				{
					Name:   "a",
					Remote: aRemoteDir,
					Path:   "a",
				},
			},
		},
	})

	root := t.TempDir()
	jiri := jiriInit(t, root)
	jiri("import", "-name", "a", "a_manifest", aRemoteDir)
	jiri("update")

	aLocalDir := filepath.Join(root, "a")
	oldRev := currentRevision(t, aLocalDir)

	// Create a new commit in manifest repository's remote.
	writeFile(t, filepath.Join(aRemoteDir, "new_file.txt"), "contents")
	runSubprocess(t, aRemoteDir, "git", "add", "new_file.txt")
	runSubprocess(t, aRemoteDir, "git", "commit", "-m", "Add new_file.txt")

	jiri("update", "-local-manifest-project", "b")
	log := runSubprocess(t, aLocalDir, "git", "log", "--pretty=oneline")
	newRev := currentRevision(t, aLocalDir)

	// Validate that the local "a" repo is still at the original commit, not the
	// new one.
	if newRev != oldRev {
		t.Errorf("Root project revision incorrect; want %q, got %q. Git log:\n%s", oldRev, newRev, log)
	}
}

// Test that it's possible to change the path of a project to a subdirectory of
// the old path, even if that subdirectory already existed.
func TestMovingProjectIntoSubdir(t *testing.T) {
	t.Parallel()

	aRemoteDir := t.TempDir()
	setupGitRepo(t, aRemoteDir, map[string]any{
		// project A contains a "src" subdirectory, which shouldn't stop us from
		// moving project A into the "src" subdirectory of its original
		// path.
		"src/foo.txt": "foo\n",
	})

	bRemoteDir := t.TempDir()
	setupGitRepo(t, bRemoteDir, map[string]any{
		"manifest": project.Manifest{
			Projects: []project.Project{
				{
					Name:   "a",
					Path:   "path_to_a",
					Remote: aRemoteDir,
				},
			},
		},
	})

	root := t.TempDir()
	jiri := jiriInit(t, root)
	jiri("import", "manifest", bRemoteDir)
	jiri("update")
	checkDirContents(t, root, []string{
		"path_to_a/src/foo.txt",
	})

	manifestPath := filepath.Join(bRemoteDir, "manifest")
	manifestContents := readFile(t, manifestPath)
	// Update project a's path from `path_to_a` to `path_to_a/src`, which is a
	// directory that already exists (but as a subdirectory of project a).
	writeFile(t, manifestPath, strings.ReplaceAll(manifestContents, "path_to_a", "path_to_a/src"))
	runSubprocess(t, bRemoteDir, "git", "commit", "-am", "Update project a's path")

	jiri("update")
	checkDirContents(t, root, []string{
		"path_to_a/src/src/foo.txt",
	})
}
