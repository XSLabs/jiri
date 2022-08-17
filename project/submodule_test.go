package project

import (
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestSubmoduleRegex(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{
			" 91d92f5732440651499ea7adfa60a362a2bade39 third_party/libc-tests (heads/main)",
			[]string{"",
				"91d92f5732440651499ea7adfa60a362a2bade39", "third_party/libc-tests"},
		},
		{
			"-91d92f5732440651499ea7adfa60a362a2bade39 third_party/libc-tests (heads/main)",
			[]string{"-",
				"91d92f5732440651499ea7adfa60a362a2bade39", "third_party/libc-tests"},
		},
		{
			"U91d92f5732440651499ea7adfa60a362a2bade39 third_party/libc-tests (heads/main)",
			[]string{"U",
				"91d92f5732440651499ea7adfa60a362a2bade39", "third_party/libc-tests"},
		},
		{
			"+91d92f5732440651499ea7adfa60a362a2bade39 third_party/libc-tests (heads/main)",
			[]string{"+",
				"91d92f5732440651499ea7adfa60a362a2bade39", "third_party/libc-tests"},
		},
	}

	for _, tc := range tests {
		t.Run(fmt.Sprint(tc), func(t *testing.T) {
			got := submoduleConfigRegex.FindStringSubmatch(tc.input)[1:]
			if !cmp.Equal(got, tc.want) {
				t.Errorf("Submodule Regex from %s returned %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}
