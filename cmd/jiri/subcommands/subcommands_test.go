// Copyright 2024 The Fuchsia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package subcommands

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/subcommands"
	"go.fuchsia.dev/jiri/cmdline"
)

func TestErrToExitStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want subcommands.ExitStatus
	}{
		{
			name: "no error",
			want: 0,
		},
		{
			name: "generic error",
			err:  fmt.Errorf("foo"),
			want: 1,
		},
		{
			name: "exit status error",
			err:  cmdline.ErrExitCode(42),
			want: 42,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc := tc
			t.Parallel()
			got := errToExitStatus(context.Background(), tc.err)
			if got != tc.want {
				t.Errorf("Wrong exit code %d, wanted %d", got, tc.want)
			}
		})
	}
}
