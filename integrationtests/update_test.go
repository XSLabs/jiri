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
	importedRemoteDir := t.TempDir()
	setupGitRepo(t, importedRemoteDir, map[string]any{
		"imported_manifest": project.Manifest{
			Projects: []project.Project{
				// The project itself.
				{
					Name:   "imported",
					Remote: importedRemoteDir,
					Path:   ".",
				},
			},
		},
		"foo.txt": "foo\n",
	})

	// Set up a remote repo that imports the above repository at the root of the
	// checkout.
	remoteDir := t.TempDir()
	setupGitRepo(t, remoteDir, map[string]any{
		"top_level_manifest": project.Manifest{
			Imports: []project.Import{
				{
					Name:     "imported",
					Manifest: "imported_manifest",
					Remote:   importedRemoteDir,
					Revision: currentRevision(t, importedRemoteDir),
					Root:     "",
				},
			},
			Projects: []project.Project{
				{
					Name:   "manifest",
					Remote: remoteDir,
					Path:   "manifest",
				},
			},
		},
	})

	root := t.TempDir()
	jiri := jiriInit(t, root)
	jiri("import", "top_level_manifest", remoteDir)
	jiri("update")

	checkDirContents(t, root, []string{
		"foo.txt",
		"imported_manifest",
		"manifest/top_level_manifest",
	})
}

func TestSuperprojectChange(t *testing.T) {
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

	root := t.TempDir()
	jiri := jiriInit(t, root, "-enable-submodules=true")
	jiri("import", "manifest", remoteDir)
	jiri("update")

	// Commit a new file to the superproject's upstream.
	writeFile(t, filepath.Join(remoteDir, "bar.txt"), "bar")
	runSubprocess(t, remoteDir, "git", "add", ".")
	runSubprocess(t, remoteDir, "git", "commit", "-m", "Add bar.txt")

	// Do an update, which should pull in the superproject changes.
	jiri("update")

	checkDirContents(t, root, []string{
		"manifest_dir/bar.txt",
		"manifest_dir/manifest",
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

// Checks that -local-manifest respects changes in a manifest in an <import>ed
// project, not just the top-level manifest project.
func TestUpdateWithLocalManifestChangeInImportedProject(t *testing.T) {
	t.Parallel()

	transitiveDepRemoteDir := t.TempDir()
	setupGitRepo(t, transitiveDepRemoteDir, map[string]any{
		"foo.txt": "foo\n",
	})

	importedRemoteDir := t.TempDir()
	setupGitRepo(t, importedRemoteDir, map[string]any{
		"imported_manifest": project.Manifest{
			Projects: []project.Project{
				{
					Name:   "imported",
					Path:   ".",
					Remote: importedRemoteDir,
				},
				{
					Name:     "transitive",
					Path:     "transitive",
					Remote:   transitiveDepRemoteDir,
					Revision: currentRevision(t, transitiveDepRemoteDir),
				},
			},
		},
	})

	// Set up a remote repo that imports the above repository at the root of the
	// checkout.
	remoteDir := t.TempDir()
	setupGitRepo(t, remoteDir, map[string]any{
		"top_level_manifest": project.Manifest{
			Imports: []project.Import{
				{
					Name:     "imported",
					Manifest: "imported_manifest",
					Remote:   importedRemoteDir,
					Revision: currentRevision(t, importedRemoteDir),
					Root:     "",
				},
			},
			Projects: []project.Project{
				{
					Name:   "manifest",
					Remote: remoteDir,
					Path:   "manifest",
				},
			},
		},
	})

	root := t.TempDir()
	jiri := jiriInit(t, root)
	jiri("import", "top_level_manifest", remoteDir)
	jiri("update")

	checkDirContents(t, root, []string{
		"manifest/top_level_manifest",
		"imported_manifest",
		"transitive/foo.txt",
	})

	// Create a new commit in the transitive dependency.
	writeFile(t, filepath.Join(transitiveDepRemoteDir, "new_file.txt"), "contents")
	runSubprocess(t, transitiveDepRemoteDir, "git", "add", "new_file.txt")
	runSubprocess(t, transitiveDepRemoteDir, "git", "commit", "-m", "Add new_file.txt")
	newTransitiveDepRevision := currentRevision(t, transitiveDepRemoteDir)

	// Edit the local manifest to point to the new revision.
	localManifestPath := filepath.Join(root, "imported_manifest")
	jiri("edit", "-project", "transitive="+newTransitiveDepRevision, localManifestPath)

	jiri("update", "-local-manifest")
	checkDirContents(t, root, []string{
		"manifest/top_level_manifest",
		"imported_manifest",
		"transitive/foo.txt",
		// TODO(olivernewman): `jiri update -local-manifest` should respect
		// manifest changes in imported repositories, not just the root manifest
		// repository, so `new_file.txt` should exist.
		// "transitive/new_file.txt",
	})
}
