// Copyright 2017 The Fuchsia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package subcommands

import (
	"os"
	"strconv"
	"testing"

	"go.fuchsia.dev/jiri/jiritest"
	"go.fuchsia.dev/jiri/project"
)

func setDefaultConfigFlags() {
	projectConfigFlags.ignore = ""
	projectConfigFlags.noUpdate = ""
	projectConfigFlags.noRebase = ""
}

func testConfig(t *testing.T, fake *jiritest.FakeJiriRoot, localProjects []project.Project) {
	p, err := project.ProjectAtPath(fake.X, localProjects[1].Path)
	if err != nil {
		t.Fatal(err)
	}
	oldConfig := p.LocalConfig
	if err := runProjectConfig(fake.X, []string{}); err != nil {
		t.Fatal(err)
	}
	if p, err = project.ProjectAtPath(fake.X, localProjects[1].Path); err != nil {
		t.Fatal(err)
	}
	newConfig := p.LocalConfig

	expectedOutput := oldConfig.Ignore
	if projectConfigFlags.ignore != "" {
		if expectedOutput, err = strconv.ParseBool(projectConfigFlags.ignore); err != nil {
			t.Fatal(err)
		}
	}
	if newConfig.Ignore != expectedOutput {
		t.Errorf("local config ignore: got %t, want %t", newConfig.Ignore, expectedOutput)
	}

	expectedOutput = oldConfig.NoUpdate
	if projectConfigFlags.noUpdate != "" {
		if expectedOutput, err = strconv.ParseBool(projectConfigFlags.noUpdate); err != nil {
			t.Fatal(err)
		}
	}
	if newConfig.NoUpdate != expectedOutput {
		t.Errorf("local config no-update: got %t, want %t", newConfig.NoUpdate, expectedOutput)
	}

	expectedOutput = oldConfig.NoRebase
	if projectConfigFlags.noRebase != "" {
		if expectedOutput, err = strconv.ParseBool(projectConfigFlags.noRebase); err != nil {
			t.Fatal(err)
		}
	}
	if newConfig.NoRebase != expectedOutput {
		t.Errorf("local config no-rebase: got %t, want %t", newConfig.NoRebase, expectedOutput)
	}
}

func TestConfig(t *testing.T) {
	localProjects, fake, cleanup := setupUniverse(t)
	if err := fake.UpdateUniverse(false); err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	currentDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(currentDir); err != nil {
			t.Fatal(err)
		}
	}()
	if err := os.Chdir(localProjects[1].Path); err != nil {
		t.Fatal(err)
	}

	setDefaultConfigFlags()
	projectConfigFlags.ignore = "true"
	testConfig(t, fake, localProjects)

	setDefaultConfigFlags()
	projectConfigFlags.noUpdate = "true"
	projectConfigFlags.noRebase = "true"
	testConfig(t, fake, localProjects)

	setDefaultConfigFlags()
	testConfig(t, fake, localProjects)

	setDefaultConfigFlags()
	projectConfigFlags.noRebase = "false"
	projectConfigFlags.ignore = "true"
	testConfig(t, fake, localProjects)

	setDefaultConfigFlags()
	projectConfigFlags.noRebase = "false"
	projectConfigFlags.noUpdate = "false"
	testConfig(t, fake, localProjects)

	setDefaultConfigFlags()
	projectConfigFlags.noRebase = "false"
	projectConfigFlags.noUpdate = "false"
	projectConfigFlags.ignore = "false"
	testConfig(t, fake, localProjects)
}
