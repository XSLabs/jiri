// Copyright 2019 The Fuchsia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package subcommands

import (
	"os"
	"path/filepath"
	"testing"

	"go.fuchsia.dev/jiri/jiritest"
	"go.fuchsia.dev/jiri/project"
)

func TestResolveProjects(t *testing.T) {
	t.Parallel()

	_, fakeroot := setupUniverse(t)

	if err := fakeroot.UpdateUniverse(false); err != nil {
		t.Fatalf("%v", err)
	}
	localProjects, err := project.LocalProjects(fakeroot.X, project.FastScan)
	projects, _, _, err := project.LoadManifestFile(fakeroot.X, fakeroot.X.JiriManifestFile(), localProjects, nil)
	lockPath := filepath.Join(fakeroot.X.Root, "jiri.lock")
	cmd := resolveCmd{
		lockFilePath:      lockPath,
		enablePackageLock: true,
		enableProjectLock: true,
	}
	args := []string{}
	if err := cmd.run(fakeroot.X, args); err != nil {
		t.Fatalf("resolve failed due to error %v", err)
	}
	data, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("%+v", err)
	}

	projLocks, _, err := project.UnmarshalLockEntries(data)
	if err != nil {
		t.Fatalf("parse generated lockfile failed due to error: %v", err)
	}

	if len(projects) != len(projLocks) {
		t.Errorf("expecting %v project locks, got %v", len(projects), len(projLocks))
	}

	for k, v := range projects {
		if projLock, ok := projLocks[project.ProjectLockKey(k)]; ok {
			if v.Revision != projLock.Revision {
				t.Errorf("expecting revision %q for project %q, got %q", v.Revision, v.Name, projLock.Revision)
			}
		} else {
			t.Errorf("project %q not found in lockfile", v.Name)
		}
	}
}

func TestResolvePackages(t *testing.T) {
	t.Parallel()

	fakeroot := jiritest.NewFakeJiriRoot(t)

	// Replace the .jiri_manifest with package declarations
	pkgData := []byte(`
<manifest>
	<packages>
		<package name="gn/gn/${platform}"
             version="git_revision:bdb0fd02324b120cacde634a9235405061c8ea06"
             path="buildtools/{{.OS}}-x64"/>
    	<package name="infra/tools/luci/vpython/${platform}"
             version="git_revision:9a931a5307c46b16b1c12e01e8239d4a73830b89"
             path="buildtools/{{.OS}}-x64"/>
	</packages>
</manifest>
`)
	// Currently jiri is hard coded to only verify cipd packages for linux-amd64 and mac-amd64.
	// If new supported platform added, this test should be updated.
	expectedLocks := []project.PackageLock{
		{
			PackageName: "gn/gn/linux-amd64",
			VersionTag:  "git_revision:bdb0fd02324b120cacde634a9235405061c8ea06",
			InstanceID:  "0uGjKAZkJXPZjtYktgEwHiNbwsut_qRsk7ZCGGxi82IC",
		},
		{
			PackageName: "gn/gn/mac-amd64",
			VersionTag:  "git_revision:bdb0fd02324b120cacde634a9235405061c8ea06",
			InstanceID:  "rN2F641yR4Bj-H1q8OwC_RiqRpUYxy3hryzRfPER9wcC",
		},
		{
			PackageName: "infra/tools/luci/vpython/linux-amd64",
			VersionTag:  "git_revision:9a931a5307c46b16b1c12e01e8239d4a73830b89",
			InstanceID:  "uCjugbKg6wMIF6_H_BHECZQdcGRebhnZ6LzSodPHQ7AC",
		},
		{
			PackageName: "infra/tools/luci/vpython/mac-amd64",
			VersionTag:  "git_revision:9a931a5307c46b16b1c12e01e8239d4a73830b89",
			InstanceID:  "yAdok-mh5vfwq1vCAHprmejM9iE7R1t9Wn6RxrWmAAEC",
		},
	}
	if err := os.WriteFile(fakeroot.X.JiriManifestFile(), pkgData, 0644); err != nil {
		t.Fatalf("failed to write package information into .jiri_manifest due to error: %v", err)
	}
	lockPath := filepath.Join(fakeroot.X.Root, "jiri.lock")
	cmd := resolveCmd{
		lockFilePath:         lockPath,
		enablePackageLock:    true,
		enableProjectLock:    true,
		enablePackageVersion: true,
	}
	args := []string{}
	if err := cmd.run(fakeroot.X, args); err != nil {
		t.Fatalf("resolve failed due to error: %v", err)
	}
	data, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("read generated lockfile failed due to error: %v", err)
	}
	_, pkgLocks, err := project.UnmarshalLockEntries(data)
	if err != nil {
		t.Fatalf("parse generated lockfile failed due to error: %v", err)
	}
	if len(expectedLocks) != len(pkgLocks) {
		t.Errorf("expecting %v locks, got %v", len(expectedLocks), len(pkgLocks))
	}
	for _, v := range expectedLocks {
		if pkgLock, ok := pkgLocks[v.Key()]; ok {
			if pkgLock != v {
				t.Errorf("expecting instance id %q for package %q, got %q", v.InstanceID, v.PackageName, pkgLock.InstanceID)
			}
		} else {
			t.Errorf("package %q not found in generated lockfile", v.PackageName)
		}
	}
}

func TestResolvePackagesPartial(t *testing.T) {
	t.Parallel()

	fakeroot := jiritest.NewFakeJiriRoot(t)

	// Replace the .jiri_manifest with package declarations
	pkgData := []byte(`
<manifest>
	<packages>
		<package name="gn/gn/${platform}"
             version="git_revision:bdb0fd02324b120cacde634a9235405061c8ea06"
             path="buildtools/{{.OS}}-x64"/>
		<package name="infra/tools/luci/vpython/${platform}"
             version="git_revision:9a931a5307c46b16b1c12e01e8239d4a73830b89"
             path="buildtools/{{.OS}}-x64"/>
	</packages>
</manifest>
`)
	lockData := []byte(`
[
	{
		"package": "gn/gn/linux-amd64",
		"version": "git_revision:bdb0fd02324b120cacde634a9235405061c8ea06",
		"instance_id": "0uGjKAZkJXPZjtYktgEwHiNbwsut_qRsk7ZCGGxi82IC"
	},
	{
		"package": "gn/gn/mac-amd64",
		"version": "git_revision:bdb0fd02324b120cacde634a9235405061c8ea06",
		"instance_id": "rN2F641yR4Bj-H1q8OwC_RiqRpUYxy3hryzRfPER9wcC"
	},
	{
		"package": "infra/tools/luci/vpython/linux-amd64",
		"version": "git_revision:d7d9ae19b9ace8164177c38a3f0afd2f698c02a7",
		"instance_id": "uiXWd9vshjd1KMvVmdopnRnfAPbWpyvqJqsWn2Rcs9kC"
	},
	{
		"package": "infra/tools/luci/vpython/mac-amd64",
		"version": "git_revision:d7d9ae19b9ace8164177c38a3f0afd2f698c02a7",
		"instance_id": "DEbIUasQv4NGfzxj9b6gYzMrZKr9kQ6mF6ZX41a_9_8C"
	}
]
`)
	// Currently jiri is hard coded to only verify cipd packages for linux-amd64 and mac-amd64.
	// If new supported platform added, this test should be updated.
	expectedLocks := []project.PackageLock{
		{
			PackageName: "gn/gn/linux-amd64",
			VersionTag:  "git_revision:bdb0fd02324b120cacde634a9235405061c8ea06",
			InstanceID:  "0uGjKAZkJXPZjtYktgEwHiNbwsut_qRsk7ZCGGxi82IC",
		},
		{
			PackageName: "gn/gn/mac-amd64",
			VersionTag:  "git_revision:bdb0fd02324b120cacde634a9235405061c8ea06",
			InstanceID:  "rN2F641yR4Bj-H1q8OwC_RiqRpUYxy3hryzRfPER9wcC",
		},
		{
			PackageName: "infra/tools/luci/vpython/linux-amd64",
			VersionTag:  "git_revision:9a931a5307c46b16b1c12e01e8239d4a73830b89",
			InstanceID:  "uCjugbKg6wMIF6_H_BHECZQdcGRebhnZ6LzSodPHQ7AC",
		},
		{
			PackageName: "infra/tools/luci/vpython/mac-amd64",
			VersionTag:  "git_revision:9a931a5307c46b16b1c12e01e8239d4a73830b89",
			InstanceID:  "yAdok-mh5vfwq1vCAHprmejM9iE7R1t9Wn6RxrWmAAEC",
		},
	}
	if err := os.WriteFile(fakeroot.X.JiriManifestFile(), pkgData, 0644); err != nil {
		t.Fatalf("failed to write package information into .jiri_manifest due to error: %v", err)
	}
	lockPath := filepath.Join(fakeroot.X.Root, "jiri.lock")
	if err := os.WriteFile(lockPath, lockData, 0644); err != nil {
		t.Fatalf("failed to write lockfile information into jiri.lock due to error: %v", err)
	}
	cmd := resolveCmd{
		lockFilePath:         lockPath,
		enablePackageLock:    true,
		enableProjectLock:    true,
		enablePackageVersion: true,
		fullResolve:          false,
	}
	args := []string{}
	if err := cmd.run(fakeroot.X, args); err != nil {
		t.Fatalf("resolve failed due to error: %v", err)
	}
	data, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("read generated lockfile failed due to error: %v", err)
	}
	_, pkgLocks, err := project.UnmarshalLockEntries(data)
	if err != nil {
		t.Fatalf("parse generated lockfile failed due to error: %v", err)
	}
	if len(expectedLocks) != len(pkgLocks) {
		t.Errorf("expecting %v locks, got %v", len(expectedLocks), len(pkgLocks))
	}
	for _, v := range expectedLocks {
		if pkgLock, ok := pkgLocks[v.Key()]; ok {
			if pkgLock != v {
				t.Errorf("expecting instance id %q for package %q, got %q", v.InstanceID, v.PackageName, pkgLock.InstanceID)
			}
		} else {
			t.Errorf("package %q not found in generated lockfile", v.PackageName)
		}
	}
}
