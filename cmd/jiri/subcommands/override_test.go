// Copyright 2018 The Fuchsia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package subcommands

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"go.fuchsia.dev/jiri/jiritest/xtest"
	"go.fuchsia.dev/jiri/project"
)

type overrideTestCase struct {
	Name           string
	Args           []string
	Exist          string
	Want           string
	WantJSONOutput string
	Stdout         string
	WantErr        string
	Flags          overrideCmd
	runOnce        bool
}

func TestOverride(t *testing.T) {
	t.Parallel()

	tests := []overrideTestCase{
		{
			Name:    "no args",
			WantErr: `wrong number of arguments`,
		},
		{
			Name:    "too few args",
			Args:    []string{"a"},
			WantErr: `wrong number of arguments`,
		},
		{
			Name:    "too many args",
			Args:    []string{"a", "b", "c"},
			WantErr: `wrong number of arguments`,
		},
		// Remote imports, default append behavior
		{
			Name: "remote imports",
			Args: []string{"foo", "https://github.com/new.git"},
			Want: `<manifest>
  <imports>
    <import manifest="manifest" name="foo" remote="https://github.com/new.git"/>
  </imports>
  <overrides>
    <project name="foo" remote="https://github.com/new.git"/>
  </overrides>
</manifest>
`,
		},
		{
			Name: "path specified",
			Flags: overrideCmd{
				path: "bar",
			},
			Args: []string{"foo", "https://github.com/new.git"},
			Want: `<manifest>
  <imports>
    <import manifest="manifest" name="foo" remote="https://github.com/new.git"/>
  </imports>
  <overrides>
    <project name="foo" path="bar" remote="https://github.com/new.git"/>
  </overrides>
</manifest>
`,
		},
		{
			Name: "revision specified",
			Flags: overrideCmd{
				revision: "bar",
			},
			Args: []string{"foo", "https://github.com/new.git"},
			Want: `<manifest>
  <imports>
    <import manifest="manifest" name="foo" remote="https://github.com/new.git"/>
  </imports>
  <overrides>
    <project name="foo" remote="https://github.com/new.git" revision="bar"/>
  </overrides>
</manifest>
`,
		},
		{
			Name: "json output",
			Flags: overrideCmd{
				list:       true,
				jsonOutput: filepath.Join(t.TempDir(), "file"),
			},
			Exist: `<manifest>
  <imports>
    <import manifest="manifest" name="orig" remote="https://github.com/orig.git"/>
  </imports>
  <overrides>
    <project name="foo" remote="https://github.com/new.git"/>
  </overrides>
</manifest>
`,
			WantJSONOutput: `[
  {
    "name": "foo",
    "remote": "https://github.com/new.git",
    "revision": "HEAD"
  }
]
`,
		},
		{
			Name: "list",
			Flags: overrideCmd{
				list: true,
			},
			Exist: `<manifest>
  <imports>
    <import manifest="manifest" name="orig" remote="https://github.com/orig.git"/>
  </imports>
  <overrides>
    <project name="foo" remote="https://github.com/new.git"/>
  </overrides>
</manifest>
`,
			Stdout: `* override foo
  Name:        foo
  Remote:      https://github.com/new.git
  Revision:    HEAD
`,
		},
		{
			Name: "existing overrides",
			Args: []string{"bar", "https://github.com/bar.git"},
			Exist: `<manifest>
  <imports>
    <import manifest="manifest" name="orig" remote="https://github.com/orig.git"/>
  </imports>
  <overrides>
    <project name="foo" remote="https://github.com/foo.git"/>
  </overrides>
</manifest>
`,
			Want: `<manifest>
  <imports>
    <import manifest="manifest" name="orig" remote="https://github.com/orig.git"/>
  </imports>
  <overrides>
    <project name="foo" remote="https://github.com/foo.git"/>
    <project name="bar" remote="https://github.com/bar.git"/>
  </overrides>
</manifest>
`,
		},
		// test delete flag
		{
			Name: "delete with too few args",
			Flags: overrideCmd{
				delete: true,
			},
			WantErr: `wrong number of arguments for the delete flag`,
			runOnce: true,
		},
		{
			Name: "delete with too many args",
			Flags: overrideCmd{
				delete: true,
			},
			Args:    []string{"a", "b", "c"},
			WantErr: `wrong number of arguments for the delete flag`,
			runOnce: true,
		},
		{
			Name: "delete with list",
			Flags: overrideCmd{
				delete: true,
				list:   true,
			},
			Args:    []string{"a", "b"},
			WantErr: `cannot use -delete and -list together`,
			runOnce: true,
		},
		{
			Name: "delete",
			Flags: overrideCmd{
				delete: true,
			},
			Args:    []string{"foo"},
			runOnce: true,
			Exist: `<manifest>
  <imports>
    <import manifest="manifest" name="orig" remote="https://github.com/orig.git"/>
  </imports>
  <overrides>
    <project name="foo" remote="https://github.com/foo.git"/>
    <project name="bar" remote="https://github.com/bar.git"/>
  </overrides>
</manifest>
`,
			Want: `<manifest>
  <imports>
    <import manifest="manifest" name="orig" remote="https://github.com/orig.git"/>
  </imports>
  <overrides>
    <project name="bar" remote="https://github.com/bar.git"/>
  </overrides>
</manifest>
`,
		},
		{
			Name: "ambiguous delete",
			Flags: overrideCmd{
				delete: true,
			},
			Args:    []string{"foo"},
			runOnce: true,
			Exist: `<manifest>
  <imports>
    <import manifest="manifest" name="orig" remote="https://github.com/orig.git"/>
  </imports>
  <overrides>
    <project name="foo" remote="https://github.com/foo.git"/>
    <project name="foo" remote="https://github.com/bar.git"/>
  </overrides>
</manifest>
`,
			WantErr: `more than one override matches`,
		},
		{
			Name: "delete specifying remote",
			Flags: overrideCmd{
				delete: true,
			},
			Args:    []string{"foo", "https://github.com/bar.git"},
			runOnce: true,
			Exist: `<manifest>
  <imports>
    <import manifest="manifest" name="orig" remote="https://github.com/orig.git"/>
  </imports>
  <overrides>
    <project name="foo" remote="https://github.com/foo.git"/>
    <project name="foo" remote="https://github.com/bar.git"/>
  </overrides>
</manifest>
`,
			Want: `<manifest>
  <imports>
    <import manifest="manifest" name="orig" remote="https://github.com/orig.git"/>
  </imports>
  <overrides>
    <project name="foo" remote="https://github.com/foo.git"/>
  </overrides>
</manifest>
`,
		},
		{
			Name: "override manifest with revision",
			Flags: overrideCmd{
				importManifest: "manifest",
				revision:       "eabeadae97b1e7f97ba93206066411adfe93a509",
			},
			Args:    []string{"orig", "https://github.com/orig.git"},
			runOnce: true,
			Exist: `<manifest>
  <imports>
    <import manifest="manifest" name="orig" remote="https://github.com/orig.git"/>
  </imports>
</manifest>
`,
			Want: `<manifest>
  <imports>
    <import manifest="manifest" name="orig" remote="https://github.com/orig.git"/>
  </imports>
  <overrides>
    <import manifest="manifest" name="orig" remote="https://github.com/orig.git" revision="eabeadae97b1e7f97ba93206066411adfe93a509"/>
  </overrides>
</manifest>
`,
		},
	}

	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			t.Parallel()
			if err := testOverride(t, test); err != nil {
				t.Errorf("%v: %v", test.Args, err)
			}
		})
	}
}

func testOverride(t *testing.T, test overrideTestCase) error {
	jirix := xtest.NewX(t)

	// Create a .jiri_manifest file which imports the manifest created above.
	manifest := project.Manifest{
		Imports: []project.Import{
			{
				Manifest: "manifest",
				Name:     "foo",
				Remote:   "https://github.com/new.git",
			},
		},
	}
	if err := manifest.ToFile(jirix, jirix.JiriManifestFile()); err != nil {
		t.Fatal(err)
	}

	filename := filepath.Join(jirix.Root, ".jiri_manifest")

	// Set up an existing file if it was specified.
	if test.Exist != "" {
		if err := os.WriteFile(filename, []byte(test.Exist), 0644); err != nil {
			return err
		}
	}

	run := func() error {
		stdout, _, err := collectStdio(jirix, test.Args, test.Flags.run)
		if err != nil {
			if test.WantErr == "" {
				return err
			}
			if got, want := err.Error(), test.WantErr; got != want {
				return fmt.Errorf("err got %q, want %q", got, want)
			}
		}
		if diff := cmp.Diff(test.Stdout, stdout); diff != "" {
			return fmt.Errorf("Unexpected diff in stdout (-want +got):\n%s", diff)
		}
		return nil
	}
	if err := run(); err != nil {
		return err
	}

	// check that it is idempotent
	if !test.runOnce {
		if err := run(); err != nil {
			return err
		}
	}

	// Make sure the right file is generated.
	if test.Want != "" {
		data, err := os.ReadFile(filename)
		if err != nil {
			return err
		}
		if diff := cmp.Diff(test.Want, string(data)); diff != "" {
			return fmt.Errorf("Unexpected diff in manifest (-want +got):\n%s", diff)
		}
	}

	// Make sure the right file is generated.
	if test.WantJSONOutput != "" {
		data, err := os.ReadFile(test.Flags.jsonOutput)
		if err != nil {
			return err
		}
		if diff := cmp.Diff(test.WantJSONOutput, string(data)); diff != "" {
			return fmt.Errorf("Unexpected diff in json output (-want +got):\n%s", diff)
		}
	}

	return nil
}
