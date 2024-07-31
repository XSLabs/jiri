// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package subcommands

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"go.fuchsia.dev/jiri/gitutil"
	"go.fuchsia.dev/jiri/jiritest"
	"go.fuchsia.dev/jiri/project"
)

func setDefaultRunpFlags() {
	runpFlags.projectKeys = ""
	runpFlags.verbose = false
	runpFlags.interactive = false
	runpFlags.uncommitted = false
	runpFlags.untracked = false
	runpFlags.noUncommitted = false
	runpFlags.noUntracked = false
	runpFlags.showNamePrefix = false
	runpFlags.showPathPrefix = false
	runpFlags.showKeyPrefix = false
	runpFlags.exitOnError = false
	runpFlags.collateOutput = true
	runpFlags.branch = ""
	runpFlags.remote = ""
}

func addProjects(t *testing.T, fake *jiritest.FakeJiriRoot) []*project.Project {
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
			t.Fatalf("%v", err)
		}
		p := project.Project{
			Name:         projectPath,
			Path:         filepath.Join(fake.X.Root, projectPath),
			Remote:       fake.Projects[projectPath],
			RemoteBranch: "main",
		}
		if err := fake.AddProject(p); err != nil {
			t.Fatalf("%v", err)
		}
		projects = append(projects, &p)
	}
	if err := fake.UpdateUniverse(false); err != nil {
		t.Fatalf("%v", err)
	}
	return projects
}

func executeRunp(t *testing.T, fake *jiritest.FakeJiriRoot, args ...string) string {
	stdout, stderr, err := collectStdio(fake.X, args, runRunp)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(strings.Join([]string{stdout, stderr}, " "))
}

func TestRunP(t *testing.T) {
	fake := jiritest.NewFakeJiriRoot(t)

	projects := addProjects(t, fake)

	if got, want := len(projects), 5; got != want {
		t.Errorf("got %d, want %d", got, want)
	}

	manifestKey := strings.Replace(projects[0].Key().String(), "r.a", "manifest", -1)
	manifestPath := strings.Replace(projects[0].Path, "r.a", "manifest", -1)
	keys := []string{manifestKey}
	paths := []string{manifestPath}
	for _, p := range projects {
		keys = append(keys, p.Key().String())
		paths = append(paths, p.Path)
	}

	fake.X.Cwd = projects[0].Path
	setDefaultRunpFlags()
	runpFlags.showNamePrefix = true
	runpFlags.verbose = true
	got := executeRunp(t, fake, "echo")
	hdr := "Project Names: manifest r.a r.b r.c sub/r.t1 sub/sub2/r.t2\n"
	hdr += "Project Keys: " + strings.Join(keys, " ") + "\n"

	if want := hdr + "manifest: \nr.a: \nr.b: \nr.c: \nsub/r.t1: \nsub/sub2/r.t2:"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	setDefaultRunpFlags()
	runpFlags.interactive = false
	got = executeRunp(t, fake, "git", "rev-parse", "--abbrev-ref", "HEAD")
	if want := "HEAD\nHEAD\nHEAD\nHEAD\nHEAD\nHEAD"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	setDefaultRunpFlags()
	runpFlags.showKeyPrefix = true
	runpFlags.interactive = false
	got = executeRunp(t, fake, "git", "rev-parse", "--abbrev-ref", "HEAD")
	if want := strings.Join(keys, ": HEAD\n") + ": HEAD"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	setDefaultRunpFlags()
	runpFlags.showNamePrefix = true
	runpFlags.interactive = false
	runpFlags.collateOutput = false
	uncollated := executeRunp(t, fake, "git", "rev-parse", "--abbrev-ref", "HEAD")
	split := strings.Split(uncollated, "\n")
	sort.Strings(split)
	got = strings.TrimSpace(strings.Join(split, "\n"))
	if want := "manifest: HEAD\nr.a: HEAD\nr.b: HEAD\nr.c: HEAD\nsub/r.t1: HEAD\nsub/sub2/r.t2: HEAD"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	setDefaultRunpFlags()
	runpFlags.showPathPrefix = true
	got = executeRunp(t, fake, "git", "rev-parse", "--abbrev-ref", "HEAD")
	if want := strings.Join(paths, ": HEAD\n") + ": HEAD"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	setDefaultRunpFlags()
	runpFlags.projectKeys = "r.t[12]"
	runpFlags.showNamePrefix = true
	got = executeRunp(t, fake, "echo")
	if want := "sub/r.t1: \nsub/sub2/r.t2:"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	rb := projects[1].Path
	rc := projects[2].Path
	t1 := projects[3].Path

	newfile := func(dir, file string) {
		testfile := filepath.Join(dir, file)
		_, err := os.Create(testfile)
		if err != nil {
			t.Errorf("failed to create %s: %v", testfile, err)
		}
	}

	git := func(dir string) *gitutil.Git {
		return gitutil.New(fake.X, gitutil.RootDirOpt(dir))
	}

	newfile(rb, "untracked.go")
	setDefaultRunpFlags()
	runpFlags.untracked = true
	runpFlags.showNamePrefix = true
	got = executeRunp(t, fake, "echo")
	if want := "r.b:"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	setDefaultRunpFlags()
	runpFlags.noUntracked = true
	runpFlags.showNamePrefix = true
	got = executeRunp(t, fake, "echo")
	if want := "manifest: \nr.a: \nr.c: \nsub/r.t1: \nsub/sub2/r.t2:"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	newfile(rc, "uncommitted.go")

	if err := git(rc).Add("uncommitted.go"); err != nil {
		t.Error(err)
	}

	setDefaultRunpFlags()
	runpFlags.uncommitted = true
	runpFlags.showNamePrefix = true
	got = executeRunp(t, fake, "echo")
	if want := "r.c:"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	setDefaultRunpFlags()
	runpFlags.noUncommitted = true
	runpFlags.showNamePrefix = true
	got = executeRunp(t, fake, "echo")
	if want := "manifest: \nr.a: \nr.b: \nsub/r.t1: \nsub/sub2/r.t2:"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	newfile(rc, "untracked.go")
	setDefaultRunpFlags()
	runpFlags.uncommitted = true
	runpFlags.untracked = true
	runpFlags.showNamePrefix = true
	got = executeRunp(t, fake, "echo")
	if want := "r.c:"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	git(rb).CreateAndCheckoutBranch("a1")
	git(rb).CreateAndCheckoutBranch("b2")
	git(rc).CreateAndCheckoutBranch("b2")
	git(t1).CreateAndCheckoutBranch("a1")

	fake.X.Cwd = rc

	setDefaultRunpFlags()
	// Just the projects with branch b2.
	runpFlags.showNamePrefix = true
	runpFlags.branch = "b2"
	got = executeRunp(t, fake, "echo")
	if want := "r.b: \nr.c:"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	setDefaultRunpFlags()
	// Show all projects even though current project is on b2
	runpFlags.showNamePrefix = true
	got = executeRunp(t, fake, "echo")
	if want := "manifest: \nr.a: \nr.b: \nr.c: \nsub/r.t1: \nsub/sub2/r.t2:"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	setDefaultRunpFlags()
	// All projects since --projects takes precedence over branches.
	runpFlags.projectKeys = ".*"
	runpFlags.showNamePrefix = true
	got = executeRunp(t, fake, "echo")
	if want := "manifest: \nr.a: \nr.b: \nr.c: \nsub/r.t1: \nsub/sub2/r.t2:"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	if err := os.MkdirAll(filepath.Join(rb, ".jiri", "a1"), os.FileMode(0755)); err != nil {
		t.Fatal(err)
	}

	setDefaultRunpFlags()
	// Just the projects with remotes containing "sub".
	runpFlags.remote = "sub"
	runpFlags.showNamePrefix = true
	got = executeRunp(t, fake, "echo")
	if want := "sub/r.t1: \nsub/sub2/r.t2:"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
