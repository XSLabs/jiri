// Copyright 2025 The Fuchsia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package project

import (
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestGetProjectsToSkip(t *testing.T) {
	// Set up a sequence of nested dependencies in the given order (A imports B,
	// B imports C, etc.).
	names := []string{"A", "B", "C", "D", "E"}
	var projects []Project
	for i, name := range names {
		proj := Project{Name: name}
		if i > 0 {
			proj.ImportedBy = names[i-1]
		}
		projects = append(projects, proj)
	}

	toSkipMap, err := getProjectsToSkip(projects, []string{"C", "E"})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for k := range toSkipMap {
		got = append(got, k.name)
	}
	slices.Sort(got)

	// A and B should be skipped because they are not in localManifestProjects,
	// nor are they reachable via localManifestProjects.
	want := []string{"A", "B"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Wrong projects to skip (-want +got):\n%s", diff)
	}
}
