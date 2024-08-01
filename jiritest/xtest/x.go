// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package xtest provides utilities for testing jiri functionality.
package xtest

import (
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"go.fuchsia.dev/jiri"
	"go.fuchsia.dev/jiri/cipd"
	"go.fuchsia.dev/jiri/cmdline"
	"go.fuchsia.dev/jiri/color"
	"go.fuchsia.dev/jiri/log"
	"go.fuchsia.dev/jiri/tool"
)

var (
	downloadCIPDOnce   sync.Once
	cipdBinaryContents []byte
)

// NewX is similar to jiri.NewX, but is meant for usage in a testing environment.
func NewX(t *testing.T) *jiri.X {
	env := cmdline.EnvFromOS()
	// Don't write test output to the global stdout/stderr, since it causes
	// noise.
	env.Stdout = io.Discard
	env.Stderr = io.Discard
	ctx := tool.NewContextFromEnv(env)
	color := color.NewColor(color.ColorNever)
	logger := log.NewLogger(log.InfoLevel, color, false, 0, time.Second*100, env.Stdout, env.Stderr)
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, jiri.RootMetaDir), 0o700); err != nil {
		t.Fatalf("TempDir() failed: %v", err)
	}
	jirix := &jiri.X{
		Context:         ctx,
		Root:            root,
		Cwd:             root,
		Jobs:            jiri.DefaultJobs,
		Color:           color,
		Logger:          logger,
		Attempts:        1,
		LockfileEnabled: false,
	}

	downloadCIPDOnce.Do(func() {
		binaryPath := filepath.Join(t.TempDir(), "cipd")
		if err := cipd.FetchBinary(jirix, binaryPath); err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(binaryPath)
		if err != nil {
			t.Fatal(err)
		}

		// Set global variable.
		cipdBinaryContents = b
	})

	if err := os.MkdirAll(filepath.Dir(jirix.CIPDPath()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(jirix.CIPDPath(), cipdBinaryContents, 0o755); err != nil {
		t.Fatal(err)
	}
	return jirix
}
