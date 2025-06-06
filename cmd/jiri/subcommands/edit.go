// Copyright 2017 The Fuchsia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package subcommands

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/google/subcommands"
	"go.fuchsia.dev/jiri"
	"go.fuchsia.dev/jiri/gitutil"
	"go.fuchsia.dev/jiri/project"
)

type editCmd struct {
	cmdBase

	projects   arrayFlag
	imports    arrayFlag
	packages   arrayFlag
	jsonOutput string
	editMode   string
}

const (
	manifest = "manifest"
	lockfile = "lockfile"
	both     = "both"
)

func (c *editCmd) Name() string     { return "edit" }
func (c *editCmd) Synopsis() string { return "Edit manifest file" }
func (c *editCmd) Usage() string {
	return `Edit manifest file by rolling the revision of provided projects, imports or packages.

Usage:
  jiri edit [flags] <manifest>

<manifest> is path of the manifest
`
}

func (c *editCmd) SetFlags(f *flag.FlagSet) {
	f.Var(&c.projects, "project", "List of projects to update. It is of form <project-name>=<revision> where revision is optional. It can be specified multiple times.")
	f.Var(&c.imports, "import", "List of imports to update. It is of form <import-name>=<revision> where revision is optional. It can be specified multiple times.")
	f.Var(&c.packages, "package", "List of packages to update. It is of form <package-name>=<version>. It can be specified multiple times.")
	f.StringVar(&c.jsonOutput, "json-output", "", "File to print changes to, in json format.")
	f.StringVar(&c.editMode, "edit-mode", "both", "Edit mode. It can be 'manifest' for updating project revisions in manifest only, 'lockfile' for updating project revisions in lockfile only or 'both' for updating project revisions in both files.")
}

func (c *editCmd) Execute(ctx context.Context, f *flag.FlagSet, _ ...any) subcommands.ExitStatus {
	return executeWrapper(ctx, c.run, c.topLevelFlags, f.Args())
}

func (c *editCmd) run(jirix *jiri.X, args []string) error {
	if len(args) != 1 {
		return jirix.UsageErrorf("Wrong number of args")
	}

	c.editMode = strings.ToLower(c.editMode)
	if c.editMode != manifest && c.editMode != lockfile && c.editMode != both {
		return fmt.Errorf("unsupported edit-mode: %q", c.editMode)
	}

	manifestPath, err := filepath.Abs(args[0])
	if err != nil {
		return err
	}
	if len(c.projects) == 0 && len(c.imports) == 0 && len(c.packages) == 0 {
		return jirix.UsageErrorf("Please provide -project, -import and/or -package flag")
	}
	projects := make(map[string]string)
	imports := make(map[string]string)
	packages := make(map[string]string)
	for _, p := range c.projects {
		s := strings.SplitN(p, "=", 2)
		if len(s) == 1 {
			projects[s[0]] = ""
		} else {
			projects[s[0]] = s[1]
		}
	}
	for _, i := range c.imports {
		s := strings.SplitN(i, "=", 2)
		if len(s) == 1 {
			imports[s[0]] = ""
		} else {
			imports[s[0]] = s[1]
		}
	}
	for _, p := range c.packages {
		// The package name may contain "=" characters; so we split the string from the rightmost "=".
		separatorPos := strings.LastIndex(p, "=")
		if separatorPos == -1 || separatorPos == 0 || separatorPos == len(p)-1 {
			return jirix.UsageErrorf("Please provide the -package flag in the form <package-name>=<version>")
		} else {
			packageName := p[:separatorPos]
			version := p[separatorPos+1:]
			packages[packageName] = version
		}
	}

	return c.updateManifest(jirix, manifestPath, projects, imports, packages)
}

func (c *editCmd) writeManifest(jirix *jiri.X, manifestPath, manifestContent string, projects map[string]string) error {
	// Create a temp dir to save backedup lockfiles
	tempDir, err := os.MkdirTemp("", "jiri_lockfile")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempDir)

	// map "backup" stores the mapping between updated lockfile with backups
	backup := make(map[string]string)
	rewind := func() {
		for k, v := range backup {
			if err := os.Rename(v, k); err != nil {
				jirix.Logger.Errorf("failed to revert changes to lockfile %q", k)
			} else {
				jirix.Logger.Debugf("reverted lockfile %q", k)
			}
		}
	}

	isLockfileDir := func(jirix *jiri.X, s string) bool {
		switch s {
		case "", ".", jirix.Root, string(filepath.Separator):
			return false
		}
		return true
	}

	if len(projects) != 0 && (c.editMode == lockfile || c.editMode == both) {
		// Search lockfiles and update
		dir := manifestPath
		for ; isLockfileDir(jirix, dir); dir = path.Dir(dir) {
			lockfile := path.Join(path.Dir(dir), jirix.LockfileName)

			if _, err := os.Stat(lockfile); err != nil {
				jirix.Logger.Debugf("lockfile could not be accessed at %q due to error %v", lockfile, err)
				continue
			}
			if err := updateLocks(jirix, tempDir, lockfile, backup, projects); err != nil {
				rewind()
				return err
			}
		}
	}

	if err := os.WriteFile(manifestPath, []byte(manifestContent), os.ModePerm); err != nil {
		rewind()
		return err
	}
	return nil
}

func updateLocks(jirix *jiri.X, tempDir, lockfile string, backup, projects map[string]string) error {
	jirix.Logger.Debugf("try updating lockfile %q", lockfile)
	bin, err := os.ReadFile(lockfile)
	if err != nil {
		return err
	}

	projectLocks, packageLocks, err := project.UnmarshalLockEntries(bin)
	if err != nil {
		return err
	}

	found := false
	for k, v := range projectLocks {
		if newRev, ok := projects[k.String()]; ok {
			v.Revision = newRev
			projectLocks[k] = v
			found = true
		}
	}

	if found {
		// backup original lockfile
		info, err := os.Stat(lockfile)
		if err != nil {
			return err
		}
		backupName := path.Join(tempDir, path.Base(lockfile))
		if err := os.WriteFile(backupName, bin, info.Mode()); err != nil {
			return err
		}
		backup[lockfile] = backupName
		ebin, err := project.MarshalLockEntries(projectLocks, packageLocks)
		if err != nil {
			return err
		}
		jirix.Logger.Debugf("updated lockfile %q", lockfile)
		return os.WriteFile(lockfile, ebin, info.Mode())
	}
	jirix.Logger.Debugf("skipped lockfile %q, no matching projects", lockfile)
	return nil
}

func updateRevision(manifestContent, tag, currentRevision, newRevision, name string) (string, error) {
	// We can do a trivial string replace if the `currentRevision` is non-empty
	// and unique. Otherwise we need to edit the entire XML block for the project.
	if currentRevision != "" && currentRevision != "HEAD" && strings.Count(manifestContent, currentRevision) == 1 {
		return strings.Replace(manifestContent, currentRevision, newRevision, 1), nil
	}
	return updateRevisionOrVersionAttr(manifestContent, tag, newRevision, name, "revision")
}

func updateVersion(manifestContent, tag string, pc packageChanges) (string, error) {
	// There are chances multiple packages share the same version tag,
	// therefore, we cannot simple replace version string globally.
	// Unlike project declaration, the version attribute of a package is not
	// allowed to be empty.
	name := regexp.QuoteMeta(pc.Name)
	oldVal := regexp.QuoteMeta(pc.OldVer)
	// Avoid using %q in regex, it behaves differently from regex.QuoteMeta.
	r, err := regexp.Compile(fmt.Sprintf("( *?)<%s[\\s\\n]+[^<]*?name=\"%s\"(.|\\n)*?version=\"%s\"(.|\\n)*?\\/>", tag, name, oldVal))
	if err != nil {
		return "", err
	}
	t := r.FindStringSubmatch(manifestContent)
	if t == nil {
		return "", fmt.Errorf("Not able to match %s \"%s\"", tag, name)
	}
	s := t[0]
	us := strings.Replace(s, fmt.Sprintf("version=\"%s\"", pc.OldVer), fmt.Sprintf("version=\"%s\"", pc.NewVer), 1)
	return strings.Replace(manifestContent, s, us, 1), nil
}

func updateRevisionOrVersionAttr(manifestContent, tag, newAttrValue, name, attr string) (string, error) {
	// Find the manifest fragment with the appropriate `name`.
	name = regexp.QuoteMeta(name)
	// Avoid using %q in regex, it behaves differently from regex.QuoteMeta.
	r, err := regexp.Compile(fmt.Sprintf("( *?)<%s[\\s\\n]+[^<]*?name=\"%s\"(.|\\n)*?\\/>", tag, name))
	if err != nil {
		return "", err
	}
	t := r.FindStringSubmatch(manifestContent)
	if t == nil {
		return "", fmt.Errorf("Not able to match %s \"%s\"", tag, name)
	}
	s := t[0]
	spaces := t[1]
	for i := 0; i < len(tag); i++ {
		spaces = spaces + " "
	}

	// Try to find the attribute `attr` in the fragment.
	r, err = regexp.Compile(fmt.Sprintf(`%s\s*=\s*"[^"]*"`, attr))
	if err != nil {
		return "", fmt.Errorf("error parsing attr regexp for: %v: %w", attr, err)
	}

	t = r.FindStringSubmatch(s)
	var rs string
	if len(t) == 0 {
		// No such attribute, add it.
		rs = strings.Replace(s, "/>", fmt.Sprintf("\n%s  %s=%q/>", spaces, attr, newAttrValue), 1)
	} else {
		// There is such an attribute, replace it.
		rs = strings.Replace(s, t[0], fmt.Sprintf(`%s="%s"`, attr, newAttrValue), 1)
	}
	// Replace entire original string s with the replacement string.
	return strings.Replace(manifestContent, s, rs, 1), nil
}

func (c *editCmd) updateManifest(jirix *jiri.X, manifestPath string, projects, imports, packages map[string]string) error {
	ec := &editChanges{
		Projects: []projectChanges{},
		Imports:  []importChanges{},
		Packages: []packageChanges{},
	}

	m, err := project.ManifestFromFile(jirix, manifestPath)
	if err != nil {
		return err
	}
	content, err := os.ReadFile(manifestPath)
	if err != nil {
		return err
	}
	manifestContent := string(content)
	editedProjects := make(map[string]string)
	scm := gitutil.New(jirix, gitutil.RootDirOpt(filepath.Dir(manifestPath)))
	for _, p := range m.Projects {
		newRevision := ""
		if rev, ok := projects[p.Name]; !ok {
			continue
		} else {
			newRevision = rev
		}
		if newRevision == "" {
			branch := "main"
			if p.RemoteBranch != "" {
				branch = p.RemoteBranch
			}
			out, err := scm.LsRemote(p.Remote, fmt.Sprintf("refs/heads/%s", branch))
			if err != nil {
				return err
			}
			newRevision = strings.Fields(string(out))[0]
		}
		if p.Revision == newRevision {
			continue
		}
		if c.editMode == manifest || c.editMode == both {
			manifestContent, err = updateRevision(manifestContent, "project", p.Revision, newRevision, p.Name)
			if err != nil {
				return err
			}
		}
		editedProjects[p.Key().String()] = newRevision
		ec.Projects = append(ec.Projects, projectChanges{
			Name:   p.Name,
			Remote: p.Remote,
			Path:   p.Path,
			OldRev: p.Revision,
			NewRev: newRevision,
		})
	}

	for _, i := range m.Imports {
		newRevision := ""
		if rev, ok := imports[i.Name]; !ok {
			continue
		} else {
			newRevision = rev
		}
		if newRevision == "" {
			branch := "main"
			if i.RemoteBranch != "" {
				branch = i.RemoteBranch
			}
			out, err := scm.LsRemote(i.Remote, fmt.Sprintf("refs/heads/%s", branch))
			if err != nil {
				return err
			}
			newRevision = strings.Fields(string(out))[0]
		}
		if i.Revision == newRevision {
			continue
		}
		manifestContent, err = updateRevision(manifestContent, "import", i.Revision, newRevision, i.Name)
		if err != nil {
			return err
		}
		ec.Imports = append(ec.Imports, importChanges{
			Name:   i.Name,
			Remote: i.Remote,
			OldRev: i.Revision,
			NewRev: newRevision,
		})
	}

	for _, p := range m.Packages {
		newVersion := ""
		if ver, ok := packages[p.Name]; !ok {
			continue
		} else {
			newVersion = ver
		}
		if newVersion == "" || p.Version == newVersion {
			continue
		}
		pc := packageChanges{
			Name:   p.Name,
			OldVer: p.Version,
			NewVer: newVersion,
		}
		manifestContent, err = updateVersion(manifestContent, "package", pc)
		if err != nil {
			return err
		}
		ec.Packages = append(ec.Packages, pc)
	}
	if c.jsonOutput != "" {
		if err := ec.toFile(c.jsonOutput); err != nil {
			return err
		}
	}

	return c.writeManifest(jirix, manifestPath, manifestContent, editedProjects)
}

type arrayFlag []string

func (i *arrayFlag) String() string {
	return strings.Join(*i, ", ")
}

func (i *arrayFlag) Set(value string) error {
	*i = append(*i, value)
	return nil
}

type projectChanges struct {
	Name   string `json:"name"`
	Remote string `json:"remote"`
	Path   string `json:"path"`
	OldRev string `json:"old_revision"`
	NewRev string `json:"new_revision"`
}

type importChanges struct {
	Name   string `json:"name"`
	Remote string `json:"remote"`
	OldRev string `json:"old_revision"`
	NewRev string `json:"new_revision"`
}

type packageChanges struct {
	Name   string `json:"name"`
	OldVer string `json:"old_version"`
	NewVer string `json:"new_version"`
}

type editChanges struct {
	Projects []projectChanges `json:"projects"`
	Imports  []importChanges  `json:"imports"`
	Packages []packageChanges `json:"packages"`
}

func (ec *editChanges) toFile(filename string) error {
	if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
		return err
	}
	out, err := json.MarshalIndent(ec, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize JSON output: %s\n", err)
	}

	err = os.WriteFile(filename, out, 0600)
	if err != nil {
		return fmt.Errorf("failed write JSON output to %s: %s\n", filename, err)
	}

	return nil
}
