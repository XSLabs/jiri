// Copyright 2018 The Fuchsia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package subcommands

import (
	"os"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"go.fuchsia.dev/jiri/cipd"
	"go.fuchsia.dev/jiri/jiritest"
)

func TestManifest(t *testing.T) {
	t.Parallel()

	// Create a test manifest file.
	testManifestFile, err := os.CreateTemp(t.TempDir(), "test_manifest")
	if err != nil {
		t.Fatalf("failed to create test manifest: %s", err)
	}
	testManifestFile.Write([]byte(`
<?xml version="1.0" encoding="UTF-8"?>
<manifest>
	<imports>
		<import name="the_import"
			manifest="the_import_manifest"
			remote="https://fuchsia.googlesource.com/the_import"
			revision="the_import_revision"
			remotebranch="the_import_remotebranch"
			root="the_import_root"/>
	</imports>
	<projects>
		<project name="the_project"
			path="path/to/the_project"
			remote="https://fuchsia.googlesource.com/the_project"
			remotebranch="the_project_remotebranch"
			revision="the_project_revision"
			githooks="the_project_githooks"
			gerrithost="https://fuchsia-review.googlesource.com"
			historydepth="2"/>
	</projects>
	<packages>
		<package name="the_package/${platform}"
			version="the_package_version"
			path="path/to/the_package"
			internal="false" />
	</packages>
</manifest>
`))
	if err := testManifestFile.Close(); err != nil {
		t.Fatal(err)
	}

	runCommand := func(t *testing.T, cmd manifestCmd, args []string) (string, error) {
		t.Helper()

		// Set up a fake Jiri root to pass to our command.
		fake := jiritest.NewFakeJiriRoot(t)

		stdout, _, err := collectStdio(fake.X, args, cmd.run)
		return stdout, err
	}

	attributeValue := func(t *testing.T, cmd manifestCmd, args ...string) string {
		stdout, err := runCommand(t, cmd, args)

		// If an error occurred, fail.
		if err != nil {
			t.Fatal(err)
		}

		return strings.Trim(stdout, " \n")
	}

	// Expects manifest to error when given args.
	expectError := func(t *testing.T, cmd manifestCmd, args ...string) {
		stdout, err := runCommand(t, cmd, args)

		// Fail if no error was output.
		if err == nil {
			t.Errorf("expected an error, got %s", stdout)
			return
		}
	}

	t.Run("should fail if manifest file is missing", func(t *testing.T) {
		t.Parallel()

		expectError(t, manifestCmd{
			ElementName: "the_import",
			Template:    "{{.Name}}",
		})

		expectError(t, manifestCmd{
			ElementName: "the_project",
			Template:    "{{.Name}}",
		})
	})

	t.Run("should fail if -attribute is missing", func(t *testing.T) {
		t.Parallel()

		expectError(t,
			manifestCmd{ElementName: "the_import"},
			testManifestFile.Name())

		expectError(t,
			manifestCmd{ElementName: "the_project"},
			testManifestFile.Name())
	})

	t.Run("should fail if -element is missing", func(t *testing.T) {
		t.Parallel()

		expectError(t,
			manifestCmd{Template: "{{.Name}}"},
			testManifestFile.Name(),
		)

		expectError(t,
			manifestCmd{Template: "{{.Name}}"},
			testManifestFile.Name(),
		)
	})

	t.Run("should read <project> attributes", func(t *testing.T) {
		t.Parallel()

		got := attributeValue(t, manifestCmd{
			ElementName: "the_project",
			Template: strings.Join([]string{
				"{{.Name}}",
				"{{.Remote}}",
				"{{.Revision}}",
				"{{.RemoteBranch}}",
				"{{.Path}}",
			}, "\n"),
		}, testManifestFile.Name())
		want := strings.Join(
			[]string{
				"the_project",
				"https://fuchsia.googlesource.com/the_project",
				"the_project_revision",
				"the_project_remotebranch",
				"path/to/the_project",
			}, "\n")
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("Unexpected diff (-want +got):\n%s", diff)
		}
	})

	t.Run("should read <import> attributes", func(t *testing.T) {
		t.Parallel()

		got := attributeValue(t, manifestCmd{
			ElementName: "the_import",
			Template: strings.Join(
				[]string{
					"{{.Name}}",
					"{{.Remote}}",
					"{{.Manifest}}",
					"{{.Revision}}",
					"{{.RemoteBranch}}",
				}, "\n",
			),
		}, testManifestFile.Name())
		want := strings.Join(
			[]string{
				"the_import",
				"https://fuchsia.googlesource.com/the_import",
				"the_import_manifest",
				"the_import_revision",
				"the_import_remotebranch",
			}, "\n",
		)

		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("Unexpected diff (-want +got):\n%s", diff)
		}
	})

	t.Run("should read <package> attributes", func(t *testing.T) {
		t.Parallel()

		got := attributeValue(t, manifestCmd{
			ElementName: "the_package/${platform}",
			Template: strings.Join(
				[]string{
					"{{.Name}}",
					"{{.Version}}",
					"{{.Path}}",
					"{{.Platforms}}",
					"{{.Internal}}",
				}, "\n",
			),
		}, testManifestFile.Name())

		var defaultPlatforms []string
		for _, p := range cipd.DefaultPlatforms() {
			defaultPlatforms = append(defaultPlatforms, p.String())
		}
		want := strings.Join(
			[]string{
				"the_package/${platform}",
				"the_package_version",
				"path/to/the_package",
				strings.Join(defaultPlatforms, ","),
				"false",
			}, "\n")

		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("Unexpected diff (-want +got):\n%s", diff)
		}
	})
}
