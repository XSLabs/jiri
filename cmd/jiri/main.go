// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// The following enables go generate to generate the doc.go file.
//go:generate go run go.fuchsia.dev/jiri/cmdline/testdata/gendoc.go -env="" .

package main

import (
	"runtime"

	"go.fuchsia.dev/jiri/cmd/jiri/subcommands"
	"go.fuchsia.dev/jiri/cmdline"
)

// cmdRoot represents the root of the jiri tool.
var cmdRoot *cmdline.Command

func init() {
	cmdRoot = subcommands.NewCmdRoot()
	runtime.GOMAXPROCS(runtime.NumCPU())
	platform_init()
}

func main() {
	cmdline.Main(cmdRoot)
}
