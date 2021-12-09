// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package textutil

import (
	"os"

	"golang.org/x/term"
)

// TerminalSize returns the dimensions of the terminal, if it's available from
// the OS, otherwise returns an error.
func TerminalSize() (row, col int, _ error) {
	// Try getting the terminal size from stdout, stderr and stdin respectively.
	// We try each of these in turn because the mechanism we're using fails if any
	// of the fds is redirected on the command line.  E.g. "tool | less" redirects
	// the stdout of tool to the stdin of less, and will mean tool cannot retrieve
	// the terminal size from stdout.
	if row, col, err := term.GetSize(int(os.Stdout.Fd())); err == nil {
		return row, col, err
	}
	if row, col, err := term.GetSize(int(os.Stderr.Fd())); err == nil {
		return row, col, err
	}
	return term.GetSize(int(os.Stdin.Fd()))
}
