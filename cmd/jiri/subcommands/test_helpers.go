// Copyright 2016 The Fuchsia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package subcommands

// This file contains helper functions related to running shell commands in tests.

import (
	"strings"

	"go.fuchsia.dev/jiri"
	"go.fuchsia.dev/jiri/tool"
)

func collectStdio(jirix *jiri.X, args []string, f func(*jiri.X, []string) error) (string, string, error) {
	var stdout, stderr strings.Builder
	jirix = jirix.Clone(tool.ContextOpts{
		Stdout: &stdout,
		Stderr: &stderr,
	})
	err := f(jirix, args)
	return stdout.String(), stderr.String(), err
}
