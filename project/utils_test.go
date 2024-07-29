package project_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"go.fuchsia.dev/jiri/jiritest"
	"go.fuchsia.dev/jiri/project"
)

func TestWriteGitExcludeFiles(t *testing.T) {
	tests := []struct {
		input       []string
		tag         string
		initialFile string
		want        string
	}{
		{
			[]string{"build/cipd.gni"},
			"package",
			"",
			"# BEGIN jiri package\n/build/cipd.gni\n# END jiri package\n",
		},
		{
			[]string{"build/cipd.gni"},
			"package",
			"# BEGIN jiri project\n/build/checkout.gni\n# END jiri project\n",
			"# BEGIN jiri project\n/build/checkout.gni\n# END jiri project\n# BEGIN jiri package\n/build/cipd.gni\n# END jiri package\n",
		},
		{
			[]string{"build/checkout.gni"},
			"project",
			"# BEGIN jiri project\n/build/checkout_test.gni\n# END jiri project\n",
			"# BEGIN jiri project\n/build/checkout.gni\n# END jiri project\n",
		},
		{
			[]string{"build/cipd.gni"},
			"package",
			"/foo/baz\n",
			"/foo/baz\n# BEGIN jiri package\n/build/cipd.gni\n# END jiri package\n",
		},
	}

	for _, tc := range tests {
		t.Run(fmt.Sprint(tc), func(t *testing.T) {
			fake := jiritest.NewFakeJiriRoot(t)

			// write initialFile
			gitExclude := filepath.Join(fake.X.Root, ".git/info/exclude")
			if err := project.SafeWriteFile(fake.X, gitExclude, []byte(tc.initialFile)); err != nil {
				t.Errorf("Unable to write initial file for testing writeGitExcludeFiles: %v", err)
			}
			if err := project.WriteGitExcludeFile(fake.X, tc.input, tc.tag); err != nil {
				t.Errorf("WritePackageFlags failed due to error: %v", err)
			}
			data, err := os.ReadFile(gitExclude)
			if err != nil {
				t.Errorf("Unable to read .git/info/exclude file: %v", err)
			}
			got := string(data)
			if !cmp.Equal(got, tc.want) {
				t.Errorf("Write git exclude files %s returned %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}
