// Copyright 2017 The Fuchsia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build !linux && !darwin
// +build !linux,!darwin

package isatty

func IsTerminal() bool {
	return true
}
