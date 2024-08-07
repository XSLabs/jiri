// Copyright 2016 The Fuchsia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package subcommands

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"go.fuchsia.dev/jiri"
	"go.fuchsia.dev/jiri/gerrit"
	"go.fuchsia.dev/jiri/gitutil"
	"go.fuchsia.dev/jiri/jiritest"
	"go.fuchsia.dev/jiri/project"
)

func projectName(i int) string {
	return fmt.Sprintf("project-%d", i)
}

// setupUniverse creates a fake jiri root with 3 remote projects.  Each project
// has a README with text "initial readme".
func setupUniverse(t *testing.T) ([]project.Project, *jiritest.FakeJiriRoot) {
	fake := jiritest.NewFakeJiriRoot(t)

	// Create some projects and add them to the remote manifest.
	numProjects := 3
	localProjects := []project.Project{}
	for i := 0; i < numProjects; i++ {
		name := projectName(i)
		path := fmt.Sprintf("path-%d", i)
		if err := fake.CreateRemoteProject(name); err != nil {
			t.Fatal(err)
		}
		p := project.Project{
			Name:   name,
			Path:   filepath.Join(fake.X.Root, path),
			Remote: fake.Projects[name],
		}
		localProjects = append(localProjects, p)
		if err := fake.AddProject(p); err != nil {
			t.Fatal(err)
		}
	}

	// Create initial commit in each repo.
	for _, remoteProjectDir := range fake.Projects {
		writeReadme(t, fake.X, remoteProjectDir, "initial readme")
	}

	return localProjects, fake
}

// setupTest creates a setup for testing the upload tool.
func setupUploadTest(t *testing.T) (*jiritest.FakeJiriRoot, []project.Project) {
	localProjects, fake := setupUniverse(t)
	if err := fake.UpdateUniverse(false); err != nil {
		t.Fatal(err)
	}
	return fake, localProjects
}

func assertUploadPushedFilesToRef(t *testing.T, jirix *jiri.X, gerritPath, pushedRef string, files []string) {
	t.Helper()

	git := gitutil.New(jirix, gitutil.RootDirOpt(gerritPath))
	if err := git.CheckoutBranch(pushedRef); err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if _, err := os.Stat(filepath.Join(gerritPath, file)); err != nil {
			if os.IsNotExist(err) {
				t.Fatalf("expected file %v to exist but it did not", file)
			}
			t.Fatalf("%v", err)
		}
		if !git.IsFileCommitted(file) {
			t.Fatalf("expected file %v to be committed but it is not", file)
		}
	}
}

func assertUploadFilesNotPushedToRef(t *testing.T, jirix *jiri.X, gerritPath, pushedRef string, files []string) {
	t.Helper()

	git := gitutil.New(jirix, gitutil.RootDirOpt(gerritPath))
	if err := git.CheckoutBranch(pushedRef); err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if _, err := os.Stat(filepath.Join(gerritPath, file)); err != nil {
			if !os.IsNotExist(err) {
				t.Fatalf("%s", err)
			}
		} else {
			t.Fatalf("expected file %v to not exist but it did", file)
		}
	}
}

func defaultUploadFlags() uploadCmd {
	return uploadCmd{
		presubmit: string(gerrit.PresubmitTestTypeAll),
		verify:    true,
	}
}

func TestUpload(t *testing.T) {
	t.Parallel()

	fake, localProjects := setupUploadTest(t)

	branch := "my-branch"
	git := gitutil.New(fake.X,
		gitutil.RootDirOpt(localProjects[1].Path),
		gitutil.UserNameOpt("John Doe"),
		gitutil.UserEmailOpt("john.doe@example.com"))
	if err := git.CreateBranchWithUpstream(branch, "origin/main"); err != nil {
		t.Fatal(err)
	}
	if err := git.CheckoutBranch(branch); err != nil {
		t.Fatal(err)
	}
	files := []string{"file1"}
	commitFiles(t, git, files)

	gerritPath := fake.Projects[localProjects[1].Name]
	fake.X.Cwd = git.RootDir()
	cmd := defaultUploadFlags()
	if err := cmd.run(fake.X, []string{}); err != nil {
		t.Fatal(err)
	}

	expectedRef := "refs/for/main"
	assertUploadPushedFilesToRef(t, fake.X, gerritPath, expectedRef, files)

	cmd.remoteBranch = "new-branch"
	cmd.setTopic = true
	if err := cmd.run(fake.X, []string{}); err != nil {
		t.Fatal(err)
	}
	topic := fmt.Sprintf("%s-%s", os.Getenv("USER"), branch)
	expectedRef = fmt.Sprintf("refs/for/%s%%topic=%s", cmd.remoteBranch, topic)

	assertUploadPushedFilesToRef(t, fake.X, gerritPath, expectedRef, files)
}

func TestUploadRef(t *testing.T) {
	t.Parallel()

	fake, localProjects := setupUploadTest(t)

	branch := "my-branch"
	git := gitutil.New(fake.X,
		gitutil.RootDirOpt(localProjects[1].Path),
		gitutil.UserNameOpt("John Doe"),
		gitutil.UserEmailOpt("john.doe@example.com"))
	if err := git.CreateBranchWithUpstream(branch, "origin/main"); err != nil {
		t.Fatal(err)
	}
	if err := git.CheckoutBranch(branch); err != nil {
		t.Fatal(err)
	}
	files := []string{"file1", "file2"}
	commitFiles(t, git, files)

	gerritPath := fake.Projects[localProjects[1].Name]
	fake.X.Cwd = git.RootDir()
	cmd := defaultUploadFlags()
	if err := cmd.run(fake.X, []string{"HEAD~1"}); err != nil {
		t.Fatal(err)
	}

	expectedRef := "refs/for/main"
	assertUploadPushedFilesToRef(t, fake.X, gerritPath, expectedRef, files[0:1])
	assertUploadFilesNotPushedToRef(t, fake.X, gerritPath, expectedRef, files[1:])
}

func TestUploadFromDetachedHead(t *testing.T) {
	t.Parallel()

	fake, localProjects := setupUploadTest(t)

	fake.X.Cwd = localProjects[1].Path

	cmd := defaultUploadFlags()
	cmd.setTopic = true
	expectedErr := "Current project is not on any branch. Either provide a topic or set flag \"-set-topic\" to false."
	if err := cmd.run(fake.X, []string{}); err == nil {
		t.Fatalf("expected error: %s", expectedErr)
	} else if err.Error() != expectedErr {
		t.Fatalf("expected error: %s\ngot error: %s", expectedErr, err)
	}

	cmd = defaultUploadFlags()
	cmd.multipart = true
	expectedErr = "Current project is not on any branch. Multipart uploads require project to be on a branch."
	if err := cmd.run(fake.X, []string{}); err == nil {
		t.Fatalf("expected a error: %s", expectedErr)
	} else if err.Error() != expectedErr {
		t.Fatalf("expected a error: %s\ngot error: %s", expectedErr, err)
	}

	cmd = defaultUploadFlags()
	if err := cmd.run(fake.X, []string{}); err != nil {
		t.Fatal(err)
	}

	cmd = defaultUploadFlags()
	cmd.topic = "topic"
	if err := cmd.run(fake.X, []string{}); err != nil {
		t.Fatal(err)
	}
}

func TestUploadMultipart(t *testing.T) {
	t.Parallel()

	fake, localProjects := setupUploadTest(t)

	branch := "my-branch"
	for i := 0; i < 2; i++ {
		git := gitutil.New(fake.X,
			gitutil.RootDirOpt(localProjects[i].Path),
			gitutil.UserNameOpt("John Doe"),
			gitutil.UserEmailOpt("john.doe@example.com"))
		if err := git.CreateBranchWithUpstream(branch, "origin/main"); err != nil {
			t.Fatal(err)
		}
		if err := git.CheckoutBranch(branch); err != nil {
			t.Fatal(err)
		}
		commitFiles(t, git, []string{"file-1" + strconv.Itoa(i)})
		commitFiles(t, git, []string{"file-2" + strconv.Itoa(i)})
	}

	gerritPath := fake.Projects[localProjects[0].Name]
	cmd := defaultUploadFlags()
	cmd.multipart = true
	fake.X.Cwd = localProjects[1].Path
	if err := cmd.run(fake.X, []string{}); err != nil {
		t.Fatal(err)
	}

	expectedRef := "refs/for/main"

	assertUploadPushedFilesToRef(t, fake.X, gerritPath, expectedRef, []string{"file-10", "file-20"})

	cmd.remoteBranch = "new-branch"

	cmd.setTopic = true
	if err := cmd.run(fake.X, []string{}); err != nil {
		t.Fatal(err)
	}
	topic := fmt.Sprintf("%s-%s", os.Getenv("USER"), branch)
	expectedRef = fmt.Sprintf("refs/for/%s%%topic=%s", cmd.remoteBranch, topic)

	assertUploadPushedFilesToRef(t, fake.X, gerritPath, expectedRef, []string{"file-10", "file-20"})
}

func TestUploadMultipartWithBranchFlagSimple(t *testing.T) {
	t.Parallel()

	fake, localProjects := setupUploadTest(t)

	branch := "my-branch"
	for i := 0; i < 2; i++ {
		git := gitutil.New(fake.X,
			gitutil.RootDirOpt(localProjects[i].Path),
			gitutil.UserNameOpt("John Doe"),
			gitutil.UserEmailOpt("john.doe@example.com"))
		if err := git.CreateBranchWithUpstream(branch, "origin/main"); err != nil {
			t.Fatalf("%v", err)
		}
		if err := git.CheckoutBranch(branch); err != nil {
			t.Fatalf("%v", err)
		}
		commitFiles(t, git, []string{"file-1" + strconv.Itoa(i)})
		commitFiles(t, git, []string{"file-2" + strconv.Itoa(i)})
	}

	gerritPath := fake.Projects[localProjects[0].Name]
	cmd := defaultUploadFlags()
	cmd.multipart = true
	cmd.branch = branch
	fake.X.Cwd = localProjects[1].Path
	if err := cmd.run(fake.X, []string{}); err != nil {
		t.Fatal(err)
	}
	expectedRef := "refs/for/main"
	assertUploadPushedFilesToRef(t, fake.X, gerritPath, expectedRef, []string{"file-10", "file-20"})

	cmd.setTopic = true
	if err := cmd.run(fake.X, []string{}); err != nil {
		t.Fatal(err)
	}
	topic := fmt.Sprintf("%s-%s", os.Getenv("USER"), branch)
	expectedRef = "refs/for/main%topic=" + topic

	assertUploadPushedFilesToRef(t, fake.X, gerritPath, expectedRef, []string{"file-10", "file-20"})
}

func TestUploadRebase(t *testing.T) {
	t.Parallel()

	fake, localProjects := setupUploadTest(t)

	branch := "my-branch"
	git := gitutil.New(fake.X,
		gitutil.RootDirOpt(localProjects[1].Path),
		gitutil.UserNameOpt("John Doe"),
		gitutil.UserEmailOpt("john.doe@example.com"))
	if err := git.Config("user.email", "john.doe@example.com"); err != nil {
		t.Fatal(err)
	}
	if err := git.Config("user.name", "John Doe"); err != nil {
		t.Fatal(err)
	}
	if err := git.CreateBranchWithUpstream(branch, "origin/main"); err != nil {
		t.Fatal(err)
	}
	if err := git.CheckoutBranch(branch); err != nil {
		t.Fatal(err)
	}
	localFiles := []string{"file1"}
	commitFiles(t, git, localFiles)

	remoteFiles := []string{"file2"}
	commitFiles(t, git, remoteFiles)

	gerritPath := fake.Projects[localProjects[1].Name]
	cmd := defaultUploadFlags()
	cmd.rebase = true
	fake.X.Cwd = localProjects[1].Path
	if err := cmd.run(fake.X, []string{}); err != nil {
		t.Fatal(err)
	}

	expectedRef := "refs/for/main"
	assertUploadPushedFilesToRef(t, fake.X, gerritPath, expectedRef, localFiles)
	assertUploadPushedFilesToRef(t, fake.X, localProjects[1].Path, branch, remoteFiles)
}

func TestUploadMultipleCommits(t *testing.T) {
	t.Parallel()

	fake, localProjects := setupUploadTest(t)

	branch := "my-branch"
	git := gitutil.New(fake.X,
		gitutil.RootDirOpt(localProjects[1].Path),
		gitutil.UserNameOpt("John Doe"),
		gitutil.UserEmailOpt("john.doe@example.com"))
	if err := git.CreateBranchWithUpstream(branch, "origin/main"); err != nil {
		t.Fatal(err)
	}
	if err := git.CheckoutBranch(branch); err != nil {
		t.Fatal(err)
	}
	files1 := []string{"file1"}
	commitFiles(t, git, files1)

	files2 := []string{"file2"}
	commitFiles(t, git, files2)

	gerritPath := fake.Projects[localProjects[1].Name]
	fake.X.Cwd = localProjects[1].Path
	cmd := defaultUploadFlags()
	if err := cmd.run(fake.X, []string{}); err != nil {
		t.Fatal(err)
	}
	expectedRef := "refs/for/main"
	assertUploadPushedFilesToRef(t, fake.X, gerritPath, expectedRef, append(files1, files2...))
}

func TestUploadUntrackedBranch(t *testing.T) {
	t.Parallel()

	fake, localProjects := setupUploadTest(t)

	branch := "my-branch"
	git := gitutil.New(fake.X,
		gitutil.RootDirOpt(localProjects[1].Path),
		gitutil.UserNameOpt("John Doe"),
		gitutil.UserEmailOpt("john.doe@example.com"))
	if err := git.CreateAndCheckoutBranch(branch); err != nil {
		t.Fatal(err)
	}
	files := []string{"file1"}
	commitFiles(t, git, files)

	gerritPath := fake.Projects[localProjects[1].Name]
	fake.X.Cwd = localProjects[1].Path
	cmd := defaultUploadFlags()
	if err := cmd.run(fake.X, []string{}); err != nil {
		t.Fatal(err)
	}
	expectedRef := "refs/for/main"

	assertUploadPushedFilesToRef(t, fake.X, gerritPath, expectedRef, files)

	cmd.remoteBranch = "new-branch"
	if err := cmd.run(fake.X, []string{}); err != nil {
		t.Fatal(err)
	}
	expectedRef = fmt.Sprintf("refs/for/%s", cmd.remoteBranch)

	assertUploadPushedFilesToRef(t, fake.X, gerritPath, expectedRef, files)
}

func TestGitOptions(t *testing.T) {
	t.Parallel()

	fake, localProjects := setupUploadTest(t)

	git := gitutil.New(fake.X,
		gitutil.RootDirOpt(localProjects[1].Path),
		gitutil.UserNameOpt("John Doe"),
		gitutil.UserEmailOpt("john.doe@example.com"))

	// Create "refs/for/main" on remote
	files := []string{"file1"}
	commitFiles(t, git, files)
	fake.X.Cwd = localProjects[1].Path
	cmd := defaultUploadFlags()
	if err := cmd.run(fake.X, []string{}); err != nil {
		t.Errorf("upload failed due to error: %v", err)
	}
	// Test passing through "--dry-run" git option
	cmd.gitOptions = "--dry-run"
	files = []string{"file2"}
	commitFiles(t, git, files)
	if err := cmd.run(fake.X, []string{}); err != nil {
		t.Errorf("upload failed due to error: %v", err)
	}
	expectedRef := "refs/for/main"
	gerritPath := fake.Projects[localProjects[1].Name]
	assertUploadFilesNotPushedToRef(t, fake.X, gerritPath, expectedRef, files)
}

// commitFile commits a file with the specified content into a branch
func commitFile(t *testing.T, git *gitutil.Git, filename string, content string) {
	t.Helper()

	if err := os.WriteFile(filepath.Join(git.RootDir(), filename), []byte(content), 0644); err != nil {
		t.Fatalf("%v", err)
	}
	commitMessage := "Commit " + filename
	if err := git.CommitFile(filename, commitMessage); err != nil {
		t.Fatalf("%v", err)
	}
}

// commitFiles commits the given files into to current branch.
func commitFiles(t *testing.T, git *gitutil.Git, filenames []string) {
	t.Helper()

	// Create and commit the files one at a time.
	for _, filename := range filenames {
		content := "This is file " + filename
		commitFile(t, git, filename, content)
	}
}
