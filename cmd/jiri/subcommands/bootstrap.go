// Copyright 2018 The Fuchsia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package subcommands

import (
	"context"
	"flag"
	"fmt"
	"strings"

	"github.com/google/subcommands"
	"go.fuchsia.dev/jiri"
	"go.fuchsia.dev/jiri/cipd"
)

type bootstrapCmd struct {
	cmdBase
}

func (c *bootstrapCmd) Name() string     { return "bootstrap" }
func (c *bootstrapCmd) Synopsis() string { return "Bootstrap essential packages" }
func (c *bootstrapCmd) Usage() string {
	return `Bootstrap essential packages such as cipd.

Usage:
  jiri bootstrap [<package ...>]

<package ...> is a list of packages that can be bootstrapped by jiri. If the list is empty, jiri will list supported packages.
`
}

func (c *bootstrapCmd) SetFlags(f *flag.FlagSet) {}

func (c *bootstrapCmd) Execute(ctx context.Context, f *flag.FlagSet, _ ...any) subcommands.ExitStatus {
	return executeWrapper(ctx, c.run, c.topLevelFlags, f.Args())
}

func (c *bootstrapCmd) run(jirix *jiri.X, args []string) error {
	if len(args) == 0 {
		// Currently it only supports cipd. We may add more packages from buildtools in the future.
		fmt.Printf("Supported package(s):\n\tcipd\n")
		return nil
	}
	for _, v := range args {
		switch strings.ToLower(v) {
		case "cipd":
			if err := cipd.Bootstrap(jirix); err != nil {
				return err
			}
			fmt.Printf("cipd bootstrapped to path:%q\n", jirix.CIPDPath())

		default:
			return fmt.Errorf("unsupported package %q", v)
		}
	}
	return nil
}
