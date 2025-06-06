// Copyright 2017 The Fuchsia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package subcommands

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"go.fuchsia.dev/jiri/jiritest"
	"go.fuchsia.dev/jiri/log"
	"go.fuchsia.dev/jiri/project"
)

func createRunHookProjects(t *testing.T, fake *jiritest.FakeJiriRoot, numProjects int) []project.Project {
	localProjects := []project.Project{}
	for i := 0; i < numProjects; i++ {
		name := fmt.Sprintf("project-%d", i)
		path := fmt.Sprintf("path-%d", i)
		if err := fake.CreateRemoteProject(name); err != nil {
			t.Fatal(err)
		}
		p := project.Project{
			Name:   name,
			Path:   filepath.Join(fake.X.Root, path),
			Remote: fake.Projects[name],
		}
		localProjects = append(localProjects, p)
		if err := fake.AddProject(p); err != nil {
			t.Fatal(err)
		}
	}
	for i, localProject := range localProjects {
		writeFile(t, fake.X, fake.Projects[localProject.Name], "file1"+strconv.Itoa(i), "file1"+strconv.Itoa(i))
	}
	return localProjects
}

func TestRunHookSimple(t *testing.T) {
	t.Parallel()

	fake := jiritest.NewFakeJiriRoot(t)
	projects := createRunHookProjects(t, fake, 1)
	err := fake.AddHook(project.Hook{Name: "hook1",
		Action:      "action.sh",
		ProjectName: projects[0].Name})

	if err != nil {
		t.Fatal(err)
	}
	if err = fake.UpdateUniverse(false); err == nil {
		t.Fatal("project update should throw error as there is no action.sh script")
	}

	if err := (&runHooksCmd{}).run(fake.X, nil); err == nil {
		t.Fatal("runhooks should throw error as there is no action.sh script")
	}
}

func TestRunHookLocalManifest(t *testing.T) {
	t.Parallel()

	fake := jiritest.NewFakeJiriRoot(t)
	projects := createRunHookProjects(t, fake, 1)
	err := fake.AddHook(project.Hook{Name: "hook1",
		Action:      "action.sh",
		ProjectName: projects[0].Name})

	if err != nil {
		t.Fatal(err)
	}
	if err = fake.UpdateUniverse(false); err == nil {
		t.Fatal("project update should throw error as there is no action.sh script")
	}

	manifest, err := fake.ReadRemoteManifest()
	if err != nil {
		t.Fatal(err)
	}
	manifest.Hooks[0].Action = "action1.sh"
	manifest.ToFile(fake.X, filepath.Join(fake.X.Root, jiritest.ManifestProjectPath, jiritest.ManifestFileName))
	buf := bytes.NewBufferString("")
	fake.X.Logger = log.NewLogger(fake.X.Logger.LoggerLevel, fake.X.Color, false, 0, 100, nil, buf)
	cmd := &runHooksCmd{attempts: 1, localManifest: true, hookTimeout: project.DefaultHookTimeout}
	if err := cmd.run(fake.X, nil); err == nil {
		t.Fatal("runhooks should throw error as there is no action.sh script")
	} else if !strings.Contains(buf.String(), "action1.sh") {
		t.Log(err)
		t.Fatalf("runhooks should throw error for action1.sh script, the error it threw: %s", buf.String())
	}
}
