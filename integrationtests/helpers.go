// Copyright 2024 The Fuchsia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package integrationtests

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/subcommands"
	jirisubcommands "go.fuchsia.dev/jiri/cmd/jiri/subcommands"
	"go.fuchsia.dev/jiri/cmdline"
	"go.fuchsia.dev/jiri/envvar"
	"go.fuchsia.dev/jiri/gitutil"
	"go.fuchsia.dev/jiri/project"
)

// jiriInit returns a function that can be called to run jiri commands.
//
// For example:
//
//	jiri := jiriInit(t, root)
//	stdout := jiri("update", "-gc")
func jiriInit(t *testing.T, root string, initArgs ...string) func(args ...string) string {
	t.Helper()

	jiri := func(args ...string) string {
		t.Helper()

		var finalArgs []string
		finalArgs = append(finalArgs, args...)

		if len(args) > 0 {
			subcommand := args[0]
			switch subcommand {
			case "update", "run-hooks", "fetch-packages":
				// Don't do retries with backoff since they make tests slower
				// and shouldn't be necessary since tests should be hermetic and
				// deterministic.
				finalArgs = append(finalArgs, "-attempts", "1")
			}
		}

		var stdout, stderr bytes.Buffer
		env := &cmdline.Env{
			Stdout: &stdout,
			Stderr: &stderr,
			Vars:   envvar.SliceToMap(os.Environ()),
		}
		args = append([]string{"--root", root}, args...)
		commander, err := jirisubcommands.NewCommander(args)
		if err != nil {
			t.Fatal(err)
		}
		retcode := cmdline.Main(env, commander)
		if retcode != subcommands.ExitSuccess {
			t.Fatalf("%q failed:\n%s",
				strings.Join(append([]string{"jiri"}, args...), " "),
				string(stderr.Bytes()),
			)
		}
		return string(stdout.Bytes())
	}

	initArgs = append([]string{"init", "-analytics-opt=false"}, initArgs...)
	initArgs = append(initArgs, root)
	jiri(initArgs...)

	return jiri
}

func runSubprocess(t *testing.T, dir string, args ...string) string {
	t.Helper()

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

func writeFile(t *testing.T, path string, contents string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func fileExists(t *testing.T, path string) bool {
	t.Helper()

	if _, err := os.Stat(path); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		return false
	}
	return true
}

func setupGitRepo(t *testing.T, dir string, files map[string]any) {
	t.Helper()

	runSubprocess(t, dir, "git", "init")

	for path, contents := range files {
		var s string
		switch x := contents.(type) {
		case []byte:
			s = string(x)
		case string:
			s = x
		case project.Manifest:
			b, err := x.ToBytes()
			if err != nil {
				t.Fatal(err)
			}
			s = string(b)
		default:
			t.Fatalf("Invalid type for git repo file %s", path)
		}

		writeFile(t, filepath.Join(dir, path), s)
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
