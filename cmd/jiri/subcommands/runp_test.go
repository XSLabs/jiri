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

	"github.com/google/go-cmp/cmp"
	"go.fuchsia.dev/jiri"
	"go.fuchsia.dev/jiri/gitutil"
	"go.fuchsia.dev/jiri/jiritest"
	"go.fuchsia.dev/jiri/project"
)

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

func executeRunp(t *testing.T, fake *jiritest.FakeJiriRoot, cmd runpCmd, args ...string) string {
	stdout, stderr, err := collectStdio(fake.X, args, cmd.run)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(strings.Join([]string{stdout, stderr}, " "))
}

func TestRunP(t *testing.T) {
	t.Parallel()

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
	cmd := runpCmd{
		cmdBase:        cmdBase{jiri.TopLevelFlags{DebugVerbose: true}},
		collateOutput:  true,
		showNamePrefix: true,
	}
	got := executeRunp(t, fake, cmd, "echo")
	hdr := "Project Names: manifest r.a r.b r.c sub/r.t1 sub/sub2/r.t2\n"
	hdr += "Project Keys: " + strings.Join(keys, " ") + "\n"

	want := hdr + "manifest: \nr.a: \nr.b: \nr.c: \nsub/r.t1: \nsub/sub2/r.t2:"
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("unexpected diff:\n%s", diff)
	}

	cmd = runpCmd{collateOutput: true}
	got = executeRunp(t, fake, cmd, "git", "rev-parse", "--abbrev-ref", "HEAD")
	if want := "HEAD\nHEAD\nHEAD\nHEAD\nHEAD\nHEAD"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	cmd = runpCmd{showKeyPrefix: true, collateOutput: true}
	got = executeRunp(t, fake, cmd, "git", "rev-parse", "--abbrev-ref", "HEAD")
	if want := strings.Join(keys, ": HEAD\n") + ": HEAD"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	cmd = runpCmd{
		showNamePrefix: true,
		collateOutput:  true,
	}
	uncollated := executeRunp(t, fake, cmd, "git", "rev-parse", "--abbrev-ref", "HEAD")
	split := strings.Split(uncollated, "\n")
	sort.Strings(split)
	got = strings.TrimSpace(strings.Join(split, "\n"))
	if want := "manifest: HEAD\nr.a: HEAD\nr.b: HEAD\nr.c: HEAD\nsub/r.t1: HEAD\nsub/sub2/r.t2: HEAD"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	cmd = runpCmd{
		showPathPrefix: true,
		collateOutput:  true,
	}
	got = executeRunp(t, fake, cmd, "git", "rev-parse", "--abbrev-ref", "HEAD")
	if diff := cmp.Diff(strings.Join(paths, ": HEAD\n")+": HEAD", got); diff != "" {
		t.Errorf("Unexpected diff (-want +got):\n%s", diff)
	}

	cmd = runpCmd{
		projectKeys:    "r.t[12]",
		showNamePrefix: true,
		collateOutput:  true,
	}
	got = executeRunp(t, fake, cmd, "echo")
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

	cmd = runpCmd{
		untracked:      true,
		showNamePrefix: true,
		collateOutput:  true,
	}
	got = executeRunp(t, fake, cmd, "echo")
	if want := "r.b:"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	cmd = runpCmd{
		noUntracked:    true,
		showNamePrefix: true,
		collateOutput:  true,
	}
	got = executeRunp(t, fake, cmd, "echo")
	if want := "manifest: \nr.a: \nr.c: \nsub/r.t1: \nsub/sub2/r.t2:"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	newfile(rc, "uncommitted.go")

	if err := git(rc).Add("uncommitted.go"); err != nil {
		t.Error(err)
	}

	cmd = runpCmd{
		uncommitted:    true,
		showNamePrefix: true,
		collateOutput:  true,
	}
	got = executeRunp(t, fake, cmd, "echo")
	if want := "r.c:"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	cmd = runpCmd{
		noUncommitted:  true,
		showNamePrefix: true,
		collateOutput:  true,
	}
	got = executeRunp(t, fake, cmd, "echo")
	if want := "manifest: \nr.a: \nr.b: \nsub/r.t1: \nsub/sub2/r.t2:"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	newfile(rc, "untracked.go")

	cmd = runpCmd{
		uncommitted:    true,
		untracked:      true,
		showNamePrefix: true,
		collateOutput:  true,
	}
	got = executeRunp(t, fake, cmd, "echo")
	if want := "r.c:"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	git(rb).CreateAndCheckoutBranch("a1")
	git(rb).CreateAndCheckoutBranch("b2")
	git(rc).CreateAndCheckoutBranch("b2")
	git(t1).CreateAndCheckoutBranch("a1")

	fake.X.Cwd = rc

	// Just the projects with branch b2.
	cmd = runpCmd{
		showNamePrefix: true,
		branch:         "b2",
		collateOutput:  true,
	}
	got = executeRunp(t, fake, cmd, "echo")
	if want := "r.b: \nr.c:"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	// Show all projects even though current project is on b2
	cmd = runpCmd{
		showNamePrefix: true,
		collateOutput:  true,
	}
	got = executeRunp(t, fake, cmd, "echo")
	if want := "manifest: \nr.a: \nr.b: \nr.c: \nsub/r.t1: \nsub/sub2/r.t2:"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	// All projects since --projects takes precedence over branches.
	cmd = runpCmd{
		projectKeys:    ".*",
		showNamePrefix: true,
		collateOutput:  true,
	}
	got = executeRunp(t, fake, cmd, "echo")
	if want := "manifest: \nr.a: \nr.b: \nr.c: \nsub/r.t1: \nsub/sub2/r.t2:"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	if err := os.MkdirAll(filepath.Join(rb, ".jiri", "a1"), os.FileMode(0755)); err != nil {
		t.Fatal(err)
	}

	// Just the projects with remotes containing "sub".
	cmd = runpCmd{
		remote:         "sub",
		showNamePrefix: true,
		collateOutput:  true,
	}
	got = executeRunp(t, fake, cmd, "echo")
	if want := "sub/r.t1: \nsub/sub2/r.t2:"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
