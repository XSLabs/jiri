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

	wantFiles := []string{
		"manifest_dir/manifest",
	}

	gotFiles := listDirRecursive(t, root)
	if diff := cmp.Diff(wantFiles, gotFiles); diff != "" {
		t.Errorf("Wrong directory contents after update (-want +got):\n%s", diff)
	}
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
	importedRevision := strings.TrimSpace(
		runSubprocess(t, importedRemoteDir, "git", "rev-parse", "HEAD"))

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
					Revision: importedRevision,
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

	wantFiles := []string{
		"foo.txt",
		"imported_manifest",
		"manifest/top_level_manifest",
	}

	gotFiles := listDirRecursive(t, root)
	if diff := cmp.Diff(wantFiles, gotFiles); diff != "" {
		t.Errorf("Wrong directory contents after update (-want +got):\n%s", diff)
	}
}
