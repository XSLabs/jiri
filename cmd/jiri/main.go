// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// The following enables go generate to generate the doc.go file.
//go:generate go run go.fuchsia.dev/jiri/cmdline/testdata/gendoc.go -env="" .

package main

import (
	"fmt"
	"os"
	"runtime"

	"go.fuchsia.dev/jiri/cmd/jiri/subcommands"
	"go.fuchsia.dev/jiri/cmdline"
)

func init() {
	runtime.GOMAXPROCS(runtime.NumCPU())
	platform_init()
}

func main() {
	env := cmdline.EnvFromOS()
	commander, err := subcommands.NewCommander(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %s", err)
	}
	os.Exit(int(cmdline.Main(env, commander)))
}
