// Copyright 2018 The Fuchsia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package subcommands

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.fuchsia.dev/jiri/jiritest/xtest"
	"go.fuchsia.dev/jiri/project"
)

type overrideTestCase struct {
	Name           string
	Args           []string
	Exist          string
	Want           string
	WantJSONOutput string
	Stdout, Stderr string
	SetFlags       func()
	runOnce        bool
}

func setDefaultOverrideFlags() {
	overrideFlags.importManifest = ""
	overrideFlags.path = ""
	overrideFlags.revision = ""
	overrideFlags.gerritHost = ""
	overrideFlags.delete = false
	overrideFlags.list = false
	overrideFlags.JSONOutput = ""
}

func TestOverride(t *testing.T) {
	tests := []overrideTestCase{
		{
			Name:   "no args",
			Stderr: `wrong number of arguments`,
		},
		{
			Name:   "too few args",
			Args:   []string{"a"},
			Stderr: `wrong number of arguments`,
		},
		{
			Name:   "too many args",
			Args:   []string{"a", "b", "c"},
			Stderr: `wrong number of arguments`,
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
			SetFlags: func() {
				overrideFlags.path = "bar"
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
			SetFlags: func() {
				overrideFlags.revision = "bar"
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
			SetFlags: func() {
				overrideFlags.list = true
				overrideFlags.JSONOutput = filepath.Join(t.TempDir(), "file")
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
			SetFlags: func() {
				overrideFlags.list = true
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
			SetFlags: func() {
				overrideFlags.delete = true
			},
			Stderr:  `wrong number of arguments`,
			runOnce: true,
		},
		{
			Name: "delete with too many args",
			SetFlags: func() {
				overrideFlags.delete = true
			},
			Args:    []string{"a", "b", "c"},
			Stderr:  `wrong number of arguments`,
			runOnce: true,
		},
		{
			Name: "delete with list",
			SetFlags: func() {
				overrideFlags.delete = true
				overrideFlags.list = true
			},
			Args:    []string{"a", "b"},
			Stderr:  `cannot use -delete and -list together`,
			runOnce: true,
		},
		{
			Name: "delete",
			SetFlags: func() {
				overrideFlags.delete = true
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
			SetFlags: func() {
				overrideFlags.delete = true
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
			Stderr: `more than one override matches`,
		},
		{
			Name: "delete specifying remote",
			SetFlags: func() {
				overrideFlags.delete = true
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
			SetFlags: func() {
				overrideFlags.importManifest = "manifest"
				overrideFlags.revision = "eabeadae97b1e7f97ba93206066411adfe93a509"
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
		var err error
		// Run override and check the results.
		overrideCmd := func() {
			setDefaultOverrideFlags()
			if test.SetFlags != nil {
				test.SetFlags()
			}
			err = runOverride(jirix, test.Args)
		}
		stdout, _, runErr := runfunc(overrideCmd)
		if runErr != nil {
			return err
		}
		stderr := ""
		if err != nil {
			stderr = err.Error()
		}
		if got, want := stdout, test.Stdout; !strings.Contains(got, want) || (got != "" && want == "") {
			return fmt.Errorf("stdout got %q, want substr %q", got, want)
		}
		if got, want := stderr, test.Stderr; !strings.Contains(got, want) || (got != "" && want == "") {
			return fmt.Errorf("stderr got %q, want substr %q", got, want)
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
		if got, want := string(data), test.Want; got != want {
			return fmt.Errorf("GOT\n%s\nWANT\n%s", got, want)
		}
	}

	// Make sure the right file is generated.
	if test.WantJSONOutput != "" {
		data, err := os.ReadFile(overrideFlags.JSONOutput)
		if err != nil {
			return err
		}
		if got, want := string(data), test.WantJSONOutput; got != want {
			return fmt.Errorf("GOT\n%s\nWANT\n%s", got, want)
		}
	}

	return nil
}
