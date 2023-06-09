package gitutil_test

import (
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"go.fuchsia.dev/jiri/gitutil"
)

func TestSumodulePaths(t *testing.T) {
	tests := []struct {
		input []string
		want  []string
	}{
		{
			[]string{" 91d92f5732440651499ea7adfa60a362a2bade39 third_party/libc-tests (heads/main)"},
			[]string{},
		},
		// Only when the un-inited submodules are updated by paths.
		{
			[]string{"-91d92f5732440651499ea7adfa60a362a2bade39 third_party/libc-tests"},
			[]string{"third_party/libc-tests"},
		},
		{
			[]string{"U91d92f5732440651499ea7adfa60a362a2bade39 third_party/libc-tests (heads/main)"},
			[]string{},
		},
		{
			[]string{"+91d92f5732440651499ea7adfa60a362a2bade39 third_party/libc-tests (heads/main)"},
			[]string{},
		},
	}

	for _, tc := range tests {
		t.Run(fmt.Sprint(tc), func(t *testing.T) {
			got, _ := gitutil.SubmodulePathFromStatus(tc.input)
			if !cmp.Equal(got, tc.want) {
				t.Errorf("Submodule Regex from %s returned %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}
