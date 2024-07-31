// Copyright 2015 The Vanadium Authors. All rights reserved.
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
)

type importTestCase struct {
	Name           string
	Args           []string
	Exist          string
	Want           string
	WantJSONOutput string
	WantErr        string
	Stdout         string
	SetFlags       func()
	runOnce        bool
}

func setDefaultImportFlags() {
	importFlags.name = "manifest"
	importFlags.remoteBranch = "main"
	importFlags.revision = ""
	importFlags.root = ""
	importFlags.overwrite = false
	importFlags.out = ""
	importFlags.delete = false
	importFlags.list = false
	importFlags.jsonOutput = ""
}

func TestImport(t *testing.T) {
	tests := []importTestCase{
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
		{
			Name: "remote imports, default append behavior",
			SetFlags: func() {
				importFlags.name = "name"
				importFlags.remoteBranch = "remotebranch"
				importFlags.root = "root"
			},
			Args: []string{"foo", "https://github.com/new.git"},
			Want: `<manifest>
  <imports>
    <import manifest="foo" name="name" remote="https://github.com/new.git" remotebranch="remotebranch" root="root"/>
  </imports>
</manifest>
`,
		},
		{
			Name: "import in new manifest",
			Args: []string{"foo", "https://github.com/new.git"},
			Want: `<manifest>
  <imports>
    <import manifest="foo" name="manifest" remote="https://github.com/new.git"/>
  </imports>
</manifest>
`,
		},
		{
			Name: "overridden output file",
			SetFlags: func() {
				importFlags.out = filepath.Join(t.TempDir(), "file")
			},
			Args: []string{"foo", "https://github.com/new.git"},
			Want: `<manifest>
  <imports>
    <import manifest="foo" name="manifest" remote="https://github.com/new.git"/>
  </imports>
</manifest>
`,
		},
		{
			Name: "output to stdout",
			SetFlags: func() {
				importFlags.out = "-"
			},
			Args: []string{"foo", "https://github.com/new.git"},
			Stdout: `<manifest>
  <imports>
    <import manifest="foo" name="manifest" remote="https://github.com/new.git"/>
  </imports>
</manifest>
`,
		},
		{
			Name: "list json output",
			SetFlags: func() {
				importFlags.list = true
				importFlags.jsonOutput = filepath.Join(t.TempDir(), "file")
			},
			Exist: `<manifest>
  <imports>
    <import manifest="bar" name="manifest" remote="https://github.com/orig.git"/>
  </imports>
</manifest>
`,
			WantJSONOutput: `[
  {
    "manifest": "bar",
    "name": "manifest",
    "remote": "https://github.com/orig.git",
    "revision": "",
    "remoteBranch": "",
    "root": ""
  }
]`,
		},
		{
			Name: "list",
			SetFlags: func() {
				importFlags.list = true
			},
			Exist: `<manifest>
  <imports>
    <import manifest="bar" name="manifest_bar" remote="https://github.com/bar.git"/>
	<import manifest="foo" name="manifest_foo" remote="https://github.com/foo.git"/>
  </imports>
</manifest>
`,
			Stdout: `* import	manifest_bar
  Manifest:	bar
  Remote:	https://github.com/bar.git
  Revision:	
  RemoteBranch:	
  Root:	
* import	manifest_foo
  Manifest:	foo
  Remote:	https://github.com/foo.git
  Revision:	
  RemoteBranch:	
  Root:	
`,
		},
		{
			Name: "import in existing manifest",
			Args: []string{"foo", "https://github.com/new.git"},
			Exist: `<manifest>
  <imports>
    <import manifest="bar" name="manifest" remote="https://github.com/orig.git"/>
  </imports>
</manifest>
`,
			Want: `<manifest>
  <imports>
    <import manifest="bar" name="manifest" remote="https://github.com/orig.git"/>
    <import manifest="foo" name="manifest" remote="https://github.com/new.git"/>
  </imports>
</manifest>
`,
		},
		{
			Name: "remote imports with overwrite",
			SetFlags: func() {
				importFlags.overwrite = true
			},
			Args: []string{"foo", "https://github.com/new.git"},
			Want: `<manifest>
  <imports>
    <import manifest="foo" name="manifest" remote="https://github.com/new.git"/>
  </imports>
</manifest>
`,
		},
		{
			Name: "overwrite to output file",
			SetFlags: func() {
				importFlags.overwrite = true
				importFlags.out = filepath.Join(t.TempDir(), "file")
			},
			Args: []string{"foo", "https://github.com/new.git"},
			Want: `<manifest>
  <imports>
    <import manifest="foo" name="manifest" remote="https://github.com/new.git"/>
  </imports>
</manifest>
`,
		},
		{
			Name: "overwrite with writing to stdout",
			SetFlags: func() {
				importFlags.overwrite = true
				importFlags.out = "-"
			},
			Args: []string{"foo", "https://github.com/new.git"},
			Stdout: `<manifest>
  <imports>
    <import manifest="foo" name="manifest" remote="https://github.com/new.git"/>
  </imports>
</manifest>
`,
		},
		{
			Name: "overwrite",
			SetFlags: func() {
				importFlags.overwrite = true
			},
			Args: []string{"foo", "https://github.com/new.git"},
			Exist: `<manifest>
  <imports>
    <import manifest="bar" name="manifest" remote="https://github.com/orig.git"/>
  </imports>
</manifest>
`,
			Want: `<manifest>
  <imports>
    <import manifest="foo" name="manifest" remote="https://github.com/new.git"/>
  </imports>
</manifest>
`,
		},
		// test delete flag
		{
			Name: "delete with no args",
			SetFlags: func() {
				importFlags.delete = true
			},
			WantErr: `wrong number of arguments with delete flag`,
			runOnce: true,
		},
		{
			Name: "delete with too many args",
			SetFlags: func() {
				importFlags.delete = true
			},
			Args:    []string{"a", "b", "c"},
			WantErr: `wrong number of arguments with delete flag`,
			runOnce: true,
		},
		{
			Name: "delete and overwrite",
			SetFlags: func() {
				importFlags.delete = true
				importFlags.overwrite = true
			},
			Args:    []string{"a", "b"},
			WantErr: `cannot use -delete and -overwrite together`,
			runOnce: true,
		},
		{
			Name: "delete",
			SetFlags: func() {
				importFlags.delete = true
			},
			Args:    []string{"foo"},
			runOnce: true,
			Exist: `<manifest>
  <imports>
    <import manifest="bar" name="manifest" remote="https://github.com/orig.git"/>
    <import manifest="foo" name="manifest" remote="https://github.com/orig.git"/>
    <import manifest="foo1" name="manifest" remote="https://github.com/orig.git"/>
  </imports>
</manifest>
`,
			Want: `<manifest>
  <imports>
    <import manifest="bar" name="manifest" remote="https://github.com/orig.git"/>
    <import manifest="foo1" name="manifest" remote="https://github.com/orig.git"/>
  </imports>
</manifest>
`,
		},
		{
			Name: "ambiguous delete",
			SetFlags: func() {
				importFlags.delete = true
			},
			Args:    []string{"foo"},
			runOnce: true,
			Exist: `<manifest>
  <imports>
    <import manifest="bar" name="manifest" remote="https://github.com/orig.git"/>
    <import manifest="foo" name="manifest" remote="https://github.com/orig.git"/>
    <import manifest="foo" name="manifest" remote="https://github1.com/orig.git"/>
  </imports>
</manifest>
`,
			WantErr: `More than 1 import meets your criteria. Please provide remote.`,
		},
		{
			Name: "delete multiple",
			SetFlags: func() {
				importFlags.delete = true
			},
			Args:    []string{"foo"},
			runOnce: true,
			Exist: `<manifest>
  <imports>
    <import manifest="bar" name="manifest" remote="https://github.com/orig.git"/>
    <import manifest="foo" name="manifest" remote="https://github.com/orig.git"/>
    <import manifest="foo" name="manifest" remote="https://github.com/orig.git"/>
  </imports>
</manifest>
`,
			Want: `<manifest>
  <imports>
    <import manifest="bar" name="manifest" remote="https://github.com/orig.git"/>
  </imports>
</manifest>
`,
		},
		{
			Name: "delete by remote",
			SetFlags: func() {
				importFlags.delete = true
			},
			Args:    []string{"foo", "https://github2.com/orig.git"},
			runOnce: true,
			Exist: `<manifest>
  <imports>
    <import manifest="bar" name="manifest" remote="https://github.com/orig.git"/>
    <import manifest="foo" name="manifest" remote="https://github.com/orig.git"/>
    <import manifest="foo" name="manifest" remote="https://github2.com/orig.git"/>
  </imports>
</manifest>
`,
			Want: `<manifest>
  <imports>
    <import manifest="bar" name="manifest" remote="https://github.com/orig.git"/>
    <import manifest="foo" name="manifest" remote="https://github.com/orig.git"/>
  </imports>
</manifest>
`,
		},
	}

	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			if err := testImport(t, test); err != nil {
				t.Errorf("%v: %v", test.Args, err)
			}
		})
	}
}

func testImport(t *testing.T, test importTestCase) error {
	jirix := xtest.NewX(t)

	// Temporary directory in which to run `jiri import`.
	tmpDir := t.TempDir()

	manifestPath := filepath.Join(jirix.Root, ".jiri_manifest")

	// Set up manfile for the local file import tests.  It should exist in both
	// the tmpDir (for ../manfile tests) and jiriRoot.
	for _, dir := range []string{tmpDir, jirix.Root} {
		if err := os.WriteFile(filepath.Join(dir, "manfile"), nil, 0644); err != nil {
			return err
		}
	}

	// Set up an existing file if it was specified.
	if test.Exist != "" {
		if err := os.WriteFile(manifestPath, []byte(test.Exist), 0644); err != nil {
			return err
		}
	}

	run := func() error {
		setDefaultImportFlags()
		if test.SetFlags != nil {
			test.SetFlags()
		}
		stdout, _, err := collectStdio(jirix, test.Args, runImport)
		if err != nil {
			if test.WantErr == "" {
				return err
			}
			if got, want := err.Error(), test.WantErr; got != want {
				return fmt.Errorf("got err %q, want substr %q", got, want)
			}
		}
		if got, want := stdout, test.Stdout; !strings.Contains(got, want) || (got != "" && want == "") {
			return fmt.Errorf("stdout got %q, want substr %q", got, want)
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
		out := importFlags.out
		if out == "" {
			out = manifestPath
		}
		data, err := os.ReadFile(out)
		if err != nil {
			return err
		}
		if got, want := string(data), test.Want; got != want {
			return fmt.Errorf("GOT\n%s\nWANT\n%s", got, want)
		}
	}

	if test.WantJSONOutput != "" {
		data, err := os.ReadFile(importFlags.jsonOutput)
		if err != nil {
			return err
		}
		if got, want := string(data), test.WantJSONOutput; got != want {
			return fmt.Errorf("GOT\n%s\nWANT\n%s", got, want)
		}
	}

	return nil
}
