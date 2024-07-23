// Copyright 2024 The Fuchsia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package integrationtests

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"go.fuchsia.dev/jiri/cmd/jiri/subcommands"
	"go.fuchsia.dev/jiri/cmdline"
	"go.fuchsia.dev/jiri/envvar"
	"go.fuchsia.dev/jiri/gitutil"
	"go.fuchsia.dev/jiri/project"
)

// newJiri returns a function that can be called to run jiri commands.
//
// For example:
//
//	jiri := newJiri(t, root)
//	stdout := jiri("update", "-gc")
func newJiri(t *testing.T, root string) func(args ...string) string {
	t.Helper()

	return func(args ...string) string {
		t.Helper()

		args = append([]string{"--root", root}, args...)

		var stdout, stderr bytes.Buffer
		env := &cmdline.Env{
			Stdout: &stdout,
			Stderr: &stderr,
			Vars:   envvar.SliceToMap(os.Environ()),
		}
		err := cmdline.ParseAndRun(subcommands.NewCmdRoot(), env, args)
		if err != nil {
			t.Fatalf("%q failed: %s\n%s",
				strings.Join(append([]string{"jiri"}, args...), " "),
				err,
				string(stderr.Bytes()),
			)
		}
		return string(stdout.Bytes())
	}
}

func runSubprocess(t *testing.T, dir string, args ...string) string {
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Env = os.Environ()
	gitConfig := map[string]string{
		"init.defaultbranch": "main",
		// Allow adding local git directories as submodules.
		"protocol.file.allow": "always",
	}
	for k, v := range gitutil.GitConfigEnvVars(gitConfig) {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		cmdline := strings.Join(args, " ")
		msg := stderr.String()
		if msg == "" && stdout.String() != "" {
			msg = stdout.String()
		}
		t.Fatalf("%q failed: %s\n%s", cmdline, err, msg)
	}
	return string(stdout.Bytes())
}

func writeFile(t *testing.T, path string, contents []byte) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
}

func setupGitRepo(t *testing.T, dir string, files map[string]any) {
	t.Helper()

	runSubprocess(t, dir, "git", "init")

	for path, contents := range files {
		var b []byte
		switch x := contents.(type) {
		case []byte:
			b = x
		case string:
			b = []byte(x)
		case project.Manifest:
			var err error
			b, err = x.ToBytes()
			if err != nil {
				t.Fatal(err)
			}
		default:
			t.Fatalf("Invalid type for git repo file %s", path)
		}

		writeFile(t, filepath.Join(dir, path), b)
	}

	runSubprocess(t, dir, "git", "add", ".")
	runSubprocess(t, dir, "git", "commit", "-m", "Initial commit")
}

func listDirRecursive(t *testing.T, dir string) []string {
	var files []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// Ignore hidden files.
		if strings.HasPrefix(d.Name(), ".") {
			if d.Type().IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.Type().IsDir() {
			return nil
		}

		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		files = append(files, rel)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(files)
	return files
}
