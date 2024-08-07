// Copyright 2017 The Fuchsia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package subcommands

import (
	"strconv"
	"testing"

	"go.fuchsia.dev/jiri/jiritest"
	"go.fuchsia.dev/jiri/project"
)

func testConfig(t *testing.T, fake *jiritest.FakeJiriRoot, localProjects []project.Project, cmd projectConfigCmd) {
	p, err := project.ProjectAtPath(fake.X, localProjects[1].Path)
	if err != nil {
		t.Fatal(err)
	}
	oldConfig := p.LocalConfig
	if err := cmd.run(fake.X, []string{}); err != nil {
		t.Fatal(err)
	}
	if p, err = project.ProjectAtPath(fake.X, localProjects[1].Path); err != nil {
		t.Fatal(err)
	}
	newConfig := p.LocalConfig

	expectedOutput := oldConfig.Ignore
	if cmd.ignore != "" {
		if expectedOutput, err = strconv.ParseBool(cmd.ignore); err != nil {
			t.Fatal(err)
		}
	}
	if newConfig.Ignore != expectedOutput {
		t.Errorf("local config ignore: got %t, want %t", newConfig.Ignore, expectedOutput)
	}

	expectedOutput = oldConfig.NoUpdate
	if cmd.noUpdate != "" {
		if expectedOutput, err = strconv.ParseBool(cmd.noUpdate); err != nil {
			t.Fatal(err)
		}
	}
	if newConfig.NoUpdate != expectedOutput {
		t.Errorf("local config no-update: got %t, want %t", newConfig.NoUpdate, expectedOutput)
	}

	expectedOutput = oldConfig.NoRebase
	if cmd.noRebase != "" {
		if expectedOutput, err = strconv.ParseBool(cmd.noRebase); err != nil {
			t.Fatal(err)
		}
	}
	if newConfig.NoRebase != expectedOutput {
		t.Errorf("local config no-rebase: got %t, want %t", newConfig.NoRebase, expectedOutput)
	}
}

func TestConfig(t *testing.T) {
	localProjects, fake := setupUniverse(t)
	if err := fake.UpdateUniverse(false); err != nil {
		t.Fatal(err)
	}
	fake.X.Cwd = localProjects[1].Path

	testConfig(t, fake, localProjects, projectConfigCmd{
		ignore: "true",
	})

	testConfig(t, fake, localProjects, projectConfigCmd{
		noUpdate: "true",
		noRebase: "true",
	})

	testConfig(t, fake, localProjects, projectConfigCmd{})

	testConfig(t, fake, localProjects, projectConfigCmd{
		noRebase: "false",
		ignore:   "true",
	})

	testConfig(t, fake, localProjects, projectConfigCmd{
		noRebase: "false",
		noUpdate: "false",
	})

	testConfig(t, fake, localProjects, projectConfigCmd{
		noRebase: "false",
		noUpdate: "false",
		ignore:   "false",
	})
}
