// Copyright 2016 The Fuchsia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package subcommands

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"go.fuchsia.dev/jiri/gitutil"
	"go.fuchsia.dev/jiri/jiritest"
	"go.fuchsia.dev/jiri/project"
)

func makeProjects(t *testing.T, fake *jiritest.FakeJiriRoot) []*project.Project {
	projects := []*project.Project{}
	for _, name := range []string{"a", "b", "c", "t1", "t2"} {
		projectPath := "r." + name
		if name == "t1" {
			projectPath = "sub/" + projectPath
		}
		if name == "t2" {
			projectPath = "sub/sub2/" + projectPath
		}
		if err := fake.CreateRemoteProject(projectPath); err != nil {
			t.Fatal(err)
		}
		p := project.Project{
			Name:         projectPath,
			Path:         filepath.Join(fake.X.Root, projectPath),
			Remote:       fake.Projects[projectPath],
			RemoteBranch: "main",
		}
		if err := fake.AddProject(p); err != nil {
			t.Fatal(err)
		}
		projects = append(projects, &p)
	}
	if err := fake.UpdateUniverse(false); err != nil {
		t.Fatal(err)
	}

	return projects
}

func expectGrep(t *testing.T, fake *jiritest.FakeJiriRoot, cmd grepCmd, args []string, expected []string) {
	cmd.h = true
	results, err := cmd.doGrep(fake.X, args)
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(results)
	sort.Strings(expected)
	if diff := cmp.Diff(expected, results, cmpopts.EquateEmpty()); diff != "" {
		t.Errorf("Unexpected grep output (-want +got):\n%s", diff)
	}
}
func setup(t *testing.T, fake *jiritest.FakeJiriRoot) {
	projects := makeProjects(t, fake)

	files := []string{
		"Shall I compare thee to a summer's day?",
		"Thou art more lovely and more temperate:",
		"And summer's lease hath all too short a date:",
		"Sometime too hot the eye of heaven shines,",
		"line with -hyphen",
	}

	if got, want := len(projects), len(files); got != want {
		t.Errorf("got %v, want %v", got, want)
	}

	for i, project := range projects {
		path := project.Path + "/file.txt"
		err := os.WriteFile(path, []byte(files[i]), 0644)
		if err != nil {
			t.Fatal(err)
		}
		git := gitutil.New(fake.X, gitutil.RootDirOpt(project.Path))
		git.Add(path)
	}
}
func TestGrep(t *testing.T) {
	t.Parallel()

	fake := jiritest.NewFakeJiriRoot(t)
	setup(t, fake)

	cmd := grepCmd{}
	expectGrep(t, fake, cmd, []string{"too"}, []string{
		"r.c/file.txt:And summer's lease hath all too short a date:",
		"sub/r.t1/file.txt:Sometime too hot the eye of heaven shines,",
	})

	expectGrep(t, fake, cmd, []string{"supercalifragilisticexpialidocious"}, []string{})
}

func TestNFlagGrep(t *testing.T) {
	t.Parallel()

	fake := jiritest.NewFakeJiriRoot(t)
	setup(t, fake)

	cmd := grepCmd{lineNumbers: true}
	expectGrep(t, fake, cmd, []string{"too"}, []string{
		"r.c/file.txt:1:And summer's lease hath all too short a date:",
		"sub/r.t1/file.txt:1:Sometime too hot the eye of heaven shines,",
	})
}

func TestWFlagGrep(t *testing.T) {
	t.Parallel()

	fake := jiritest.NewFakeJiriRoot(t)
	setup(t, fake)

	cmd := grepCmd{
		wordBoundaries:  true,
		caseInsensitive: true,
	}
	expectGrep(t, fake, cmd, []string{"i"}, []string{
		"r.a/file.txt:Shall I compare thee to a summer's day?",
	})
}

func TestEFlagGrep(t *testing.T) {
	t.Parallel()

	fake := jiritest.NewFakeJiriRoot(t)
	setup(t, fake)

	cmd := grepCmd{
		pattern: "-hyp",
	}
	expectGrep(t, fake, cmd, []string{}, []string{
		"sub/sub2/r.t2/file.txt:line with -hyphen",
	})
}

func TestIFlagGrep(t *testing.T) {
	t.Parallel()

	fake := jiritest.NewFakeJiriRoot(t)
	setup(t, fake)

	cmd := grepCmd{}
	expectGrep(t, fake, cmd, []string{"and"}, []string{
		"r.b/file.txt:Thou art more lovely and more temperate:",
	})

	cmd.caseInsensitive = true
	expectGrep(t, fake, cmd, []string{"and"}, []string{
		"r.b/file.txt:Thou art more lovely and more temperate:",
		"r.c/file.txt:And summer's lease hath all too short a date:",
	})
}

func TestLFlagGrep(t *testing.T) {
	t.Parallel()

	fake := jiritest.NewFakeJiriRoot(t)
	setup(t, fake)

	cmd := grepCmd{filenamesOnly: true}
	expectGrep(t, fake, cmd, []string{"too"}, []string{
		"r.c/file.txt",
		"sub/r.t1/file.txt",
	})

	cmd = grepCmd{nonmatchingFilenamesOnly: true}
	expectGrep(t, fake, cmd, []string{"too"}, []string{
		"manifest/public",
		"r.a/file.txt",
		"r.b/file.txt",
		"sub/sub2/r.t2/file.txt",
	})
}
