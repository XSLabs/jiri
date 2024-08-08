// Copyright 2017 The Fuchsia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package project

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"go.fuchsia.dev/jiri"
	"go.fuchsia.dev/jiri/gitutil"
	"go.fuchsia.dev/jiri/osutil"
	"go.fuchsia.dev/jiri/retry"
)

func isFile(file string) (bool, error) {
	fileInfo, err := os.Stat(file)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmtError(err)
	}
	return !fileInfo.IsDir(), nil
}

func fmtError(err error) error {
	if err == nil {
		return nil
	}
	_, file, line, _ := runtime.Caller(1)
	return fmt.Errorf("%s:%d: %s", filepath.Base(file), line, err)
}

// errFromChannel converts a channel of errors into a single error using
// errors.Join().
func errFromChannel(c <-chan error) error {
	var errs []error
	for err := range c {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func SafeWriteFile(jirix *jiri.X, filename string, data []byte) error {
	tmp := filename + ".tmp"
	if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
		return fmtError(err)
	}
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return fmtError(err)
	}
	return fmtError(osutil.Rename(tmp, filename))
}

func isPathDir(dir string) bool {
	if dir != "" {
		if fi, err := os.Stat(dir); err == nil {
			return fi.IsDir()
		}
	}
	return false
}

func isEmpty(path string) (bool, error) {
	dir, err := os.Open(path)
	if err != nil {
		return false, fmtError(err)
	}
	defer dir.Close()

	if _, err = dir.Readdirnames(1); err != nil && err == io.EOF {
		return true, nil
	} else {
		return false, fmtError(err)
	}
}

// fmtRevision returns the first 8 chars of a revision hash.
func fmtRevision(r string) string {
	l := 8
	if len(r) < l {
		return r
	}
	return r[:l]
}

// clone is a wrapper that reattempts a git clone operation on failure.
func clone(jirix *jiri.X, repo, path string, opts ...gitutil.CloneOpt) error {
	msg := fmt.Sprintf("Cloning %s", repo)
	t := jirix.Logger.TrackTime(msg)
	defer t.Done()
	return retry.Function(jirix, func() error {
		return gitutil.New(jirix).Clone(repo, path, opts...)
	}, msg, retry.AttemptsOpt(jirix.Attempts))
}

// fetch is a wrapper that reattempts a git fetch operation on failure.
func fetch(jirix *jiri.X, path, remote string, opts ...gitutil.FetchOpt) error {
	msg := fmt.Sprintf("Fetching for %s", path)
	t := jirix.Logger.TrackTime(msg)
	defer t.Done()
	opts = append([]gitutil.FetchOpt{gitutil.RecurseSubmodulesOpt(jirix.EnableSubmodules)}, opts...)
	return retry.Function(jirix, func() error {
		return gitutil.New(jirix, gitutil.RootDirOpt(path)).Fetch(remote, opts...)
	}, msg, retry.AttemptsOpt(jirix.Attempts))
}

// WriteGitExcludeFile adds file to .git/info/exclude. Check if it exists already first.
func WriteGitExcludeFile(jirix *jiri.X, files []string, tag string) error {
	// If the file doesn't exist, create it, or append to the file
	gitExclude := filepath.Join(jirix.Root, ".git/info/exclude")
	var gitData []byte
	if _, err := os.Stat(gitExclude); err == nil {
		if gitData, err = os.ReadFile(gitExclude); err != nil {
			return err
		}
	}
	s := string(gitData)
	// Make path start from root of checkout, and end with new line.
	for i, v := range files {
		files[i] = fmt.Sprintf("/%s\n", v)
	}
	fileString := strings.Join(files, "")
	beginTag := fmt.Sprintf("# BEGIN jiri %s\n", tag)
	endTag := fmt.Sprintf("# END jiri %s\n", tag)
	m := regexp.MustCompile(fmt.Sprintf("(?s)%s(.*)%s", beginTag, endTag))
	data := fmt.Sprintf("%s%s%s", beginTag, fileString, endTag)
	if strings.Contains(s, beginTag) {
		s = m.ReplaceAllString(s, data)
	} else {
		if strings.HasSuffix(s, "\n") || (s == "") {
			s = fmt.Sprintf("%s%s", s, data)
		} else {
			s = fmt.Sprintf("%s\n%s", s, data)
		}
	}
	return SafeWriteFile(jirix, gitExclude, []byte(s))
}
