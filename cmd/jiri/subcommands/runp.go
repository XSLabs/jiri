// Copyright 2015 The Vanadium Authors, All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package subcommands

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/google/subcommands"
	"go.fuchsia.dev/jiri"
	"go.fuchsia.dev/jiri/envvar"
	"go.fuchsia.dev/jiri/project"
	"go.fuchsia.dev/jiri/simplemr"
	"go.fuchsia.dev/jiri/tool"
)

// TODO(https://fxbug.dev/356134056): delete when finished migrating to
// subcommands library.
var (
	runpFlags runpCmd
	cmdRunp   = commandFromSubcommand(&runpFlags)
)

// TODO(https://fxbug.dev/356134056): delete when finished migrating to
// subcommands library.
func init() {
	runpFlags.SetFlags(&cmdRunp.Flags)
}

type runpCmd struct {
	cmdBase

	projectKeys    string
	interactive    bool
	uncommitted    bool
	noUncommitted  bool
	untracked      bool
	noUntracked    bool
	showNamePrefix bool
	showPathPrefix bool
	showKeyPrefix  bool
	exitOnError    bool
	collateOutput  bool
	branch         string
	remote         string
}

func (c *runpCmd) Name() string     { return "runp" }
func (c *runpCmd) Synopsis() string { return "Run a command in parallel across jiri projects" }
func (c *runpCmd) Usage() string {
	return `Run a command in parallel across one or more jiri projects. Commands are run
using the shell specified by the users $SHELL environment variable, or "sh"
if that's not set. Thus commands are run as $SHELL -c "args..."

Usage:
  jiri runp [flags] <command line>

<command line> A command line to be run in each project specified by the supplied command
line flags. Any environment variables intended to be evaluated when the
command line is run must be quoted to avoid expansion before being passed to
runp by the shell.
`
}

func (c *runpCmd) SetFlags(f *flag.FlagSet) {
	f.StringVar(&c.projectKeys, "projects", "", "A Regular expression specifying project keys to run commands in. By default, runp will use projects that have the same branch checked as the current project unless it is run from outside of a project in which case it will default to using all projects.")
	f.BoolVar(&c.uncommitted, "uncommitted", false, "Match projects that have uncommitted changes")
	f.BoolVar(&c.noUncommitted, "no-uncommitted", false, "Match projects that have no uncommitted changes")
	f.BoolVar(&c.untracked, "untracked", false, "Match projects that have untracked files")
	f.BoolVar(&c.noUntracked, "no-untracked", false, "Match projects that have no untracked files")
	f.BoolVar(&c.interactive, "interactive", false, "If set, the command to be run is interactive and should not have its stdout/stderr manipulated. This flag cannot be used with -show-name-prefix, -show-key-prefix or -collate-stdout.")
	f.BoolVar(&c.showNamePrefix, "show-name-prefix", false, "If set, each line of output from each project will begin with the name of the project followed by a colon. This is intended for use with long running commands where the output needs to be streamed. Stdout and stderr are spliced apart. This flag cannot be used with -interactive, -show-path-prefix, -show-key-prefix or -collate-stdout.")
	f.BoolVar(&c.showPathPrefix, "show-path-prefix", false, "If set, each line of output from each project will begin with the path of the project followed by a colon. This is intended for use with long running commands where the output needs to be streamed. Stdout and stderr are spliced apart. This flag cannot be used with -interactive, -show-name-prefix, -show-key-prefix or -collate-stdout.")
	f.BoolVar(&c.showKeyPrefix, "show-key-prefix", false, "If set, each line of output from each project will begin with the key of the project followed by a colon. This is intended for use with long running commands where the output needs to be streamed. Stdout and stderr are spliced apart. This flag cannot be used with -interactive, -show-name-prefix, -show-path-prefix or -collate-stdout")
	f.BoolVar(&c.collateOutput, "collate-stdout", true, "Collate all stdout output from each parallel invocation and display it as if had been generated sequentially. This flag cannot be used with -show-name-prefix, -show-key-prefix or -interactive.")
	f.BoolVar(&c.exitOnError, "exit-on-error", false, "If set, all commands will killed as soon as one reports an error, otherwise, each will run to completion.")
	f.StringVar(&c.branch, "branch", "", "A regular expression specifying branch names to use in matching projects. A project will match if the specified branch exists, even if it is not checked out.")
	f.StringVar(&c.remote, "remote", "", "A Regular expression specifying projects to run commands in by matching against their remote URLs.")
}

func (c *runpCmd) Execute(ctx context.Context, f *flag.FlagSet, _ ...any) subcommands.ExitStatus {
	return executeWrapper(ctx, c.run, c.topLevelFlags, f.Args())
}

type mapInput struct {
	project.Project
	key          project.ProjectKey
	jirix        *jiri.X
	index, total int
	result       error
}

func newmapInput(jirix *jiri.X, project project.Project, key project.ProjectKey, index, total int) *mapInput {
	return &mapInput{
		Project: project,
		key:     key,
		jirix:   jirix.Clone(tool.ContextOpts{}),
		index:   index,
		total:   total,
	}
}

func projectNames(mapInputs map[project.ProjectKey]*mapInput) []string {
	n := []string{}
	for _, mi := range mapInputs {
		n = append(n, mi.Project.Name)
	}
	sort.Strings(n)
	return n
}

func projectKeys(mapInputs map[project.ProjectKey]*mapInput) []string {
	n := []string{}
	for key := range mapInputs {
		n = append(n, key.String())
	}
	sort.Strings(n)
	return n
}

type runner struct {
	jirix                *jiri.X
	args                 []string
	serializedWriterLock sync.Mutex
	collatedOutputLock   sync.Mutex
	interactive          bool
	collateOutput        bool
	exitOnError          bool
	showNamePrefix       bool
	showKeyPrefix        bool
	showPathPrefix       bool
}

func (r *runner) serializedWriter(w io.Writer) io.Writer {
	return &sharedLockWriter{&r.serializedWriterLock, w}
}

type sharedLockWriter struct {
	mu *sync.Mutex
	f  io.Writer
}

func (lw *sharedLockWriter) Write(d []byte) (int, error) {
	lw.mu.Lock()
	defer lw.mu.Unlock()
	return lw.f.Write(d)
}

func copyWithPrefix(prefix string, w io.Writer, r io.Reader) {
	reader := bufio.NewReader(r)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if line != "" {
				fmt.Fprintf(w, "%v: %v\n", prefix, line)
			}
			break
		}
		fmt.Fprintf(w, "%v: %v", prefix, line)
	}
}

type mapOutput struct {
	mi             *mapInput
	outputFilename string
	key            string
	err            error
}

func (r *runner) Map(mr *simplemr.MR, key string, val any) error {
	mi := val.(*mapInput)
	output := &mapOutput{
		key: key,
		mi:  mi}
	jirix := r.jirix
	path := os.Getenv("SHELL")
	if path == "" {
		path = "sh"
	}
	var wg sync.WaitGroup
	cmd := exec.Command(path, "-c", strings.Join(r.args, " "))
	cmd.Env = envvar.MapToSlice(jirix.Env())
	cmd.Dir = mi.Project.Path
	cmd.Stdin = mi.jirix.Stdin()
	var stdoutCloser, stderrCloser io.Closer
	if r.interactive {
		cmd.Stdout = jirix.Stdout()
		cmd.Stderr = jirix.Stdout()
	} else {
		var stdout io.Writer
		stderr := r.serializedWriter(jirix.Stderr())
		var cleanup func()
		if r.collateOutput {
			// Write standard output to a file, stderr
			// is not collated.
			f, err := os.CreateTemp("", "jiri-runp-")
			if err != nil {
				return err
			}
			stdout = f
			output.outputFilename = f.Name()
			cleanup = func() {
				os.Remove(output.outputFilename)
			}
			// The child process will have exited by the
			// time this method returns so it's safe to close the file
			// here.
			defer f.Close()
		} else {
			stdout = r.serializedWriter(jirix.Stdout())
			cleanup = func() {}
		}
		if !r.showNamePrefix && !r.showKeyPrefix && !r.showPathPrefix {
			// write directly to stdout, stderr if there's no prefix
			cmd.Stdout = stdout
			cmd.Stderr = stderr
		} else {
			stdoutReader, stdoutWriter, err := os.Pipe()
			if err != nil {
				cleanup()
				return err
			}
			stderrReader, stderrWriter, err := os.Pipe()
			if err != nil {
				cleanup()
				stdoutReader.Close()
				stdoutWriter.Close()
				return err
			}
			cmd.Stdout = stdoutWriter
			cmd.Stderr = stderrWriter
			// Record the write end of the pipe so that it can be closed
			// after the child has exited, this ensures that all goroutines
			// will finish.
			stdoutCloser = stdoutWriter
			stderrCloser = stderrWriter
			prefix := key
			if r.showNamePrefix {
				prefix = mi.Project.Name
			}
			if r.showPathPrefix {
				prefix = mi.Project.Path
			}
			wg.Add(2)
			go func() { copyWithPrefix(prefix, stdout, stdoutReader); wg.Done() }()
			go func() { copyWithPrefix(prefix, stderr, stderrReader); wg.Done() }()

		}
	}
	if err := cmd.Start(); err != nil {
		mi.result = err
	}
	done := make(chan error)
	go func() {
		done <- cmd.Wait()
	}()
	select {
	case output.err = <-done:
		if output.err != nil && r.exitOnError {
			mr.Cancel()
		}
	case <-mr.CancelCh():
		output.err = cmd.Process.Kill()
	}
	for _, closer := range []io.Closer{stdoutCloser, stderrCloser} {
		if closer != nil {
			closer.Close()
		}
	}
	wg.Wait()
	mr.MapOut(key, output)
	return nil
}

func (r *runner) Reduce(mr *simplemr.MR, key string, values []any) error {
	for _, v := range values {
		mo := v.(*mapOutput)
		if mo.err != nil {
			fmt.Fprintf(r.jirix.Stdout(), "FAILED: %v: %s %v\n", mo.key, strings.Join(r.args, " "), mo.err)
			return nil
		} else {
			if r.collateOutput {
				r.collatedOutputLock.Lock()
				defer r.collatedOutputLock.Unlock()
				defer os.Remove(mo.outputFilename)
				if fi, err := os.Open(mo.outputFilename); err == nil {
					io.Copy(r.jirix.Stdout(), fi)
					fi.Close()
				} else {
					return err
				}
			}
		}
	}
	return nil
}

func (c *runpCmd) run(jirix *jiri.X, args []string) error {
	if c.interactive {
		c.collateOutput = false
	}

	var keysRE, branchRE, remoteRE *regexp.Regexp
	var err error

	if c.projectKeys != "" {
		re := ""
		for _, pre := range strings.Split(c.projectKeys, ",") {
			re += pre + "|"
		}
		re = strings.TrimRight(re, "|")
		keysRE, err = regexp.Compile(re)
		if err != nil {
			return fmt.Errorf("failed to compile projects regexp: %q: %v", c.projectKeys, err)
		}
	}

	if c.branch != "" {
		branchRE, err = regexp.Compile(c.branch)
		if err != nil {
			return fmt.Errorf("failed to compile has-branch regexp: %q: %v", c.branch, err)
		}
	}

	if c.remote != "" {
		remoteRE, err = regexp.Compile(c.remote)
		if err != nil {
			return fmt.Errorf("failed to compile remotes regexp: %q: %v", c.remote, err)
		}
	}

	if (c.showKeyPrefix || c.showNamePrefix || c.showPathPrefix) && c.interactive {
		fmt.Fprintf(jirix.Stderr(), "WARNING: interactive mode being disabled because show-key-prefix or show-name-prefix or show-path-prefix was set\n")
		c.interactive = false
		c.collateOutput = true
	}

	dir := jirix.Cwd
	if dir == jirix.Root || err != nil {
		// jiri was run from outside of a project. Let's assume we'll
		// use all projects if none have been specified via the projects flag.
		if keysRE == nil {
			keysRE = regexp.MustCompile(".*")
		}
	}
	projects, err := project.LocalProjects(jirix, project.FastScan)
	if err != nil {
		return err
	}

	projectStateRequired := branchRE != nil || c.untracked || c.noUntracked || c.uncommitted || c.noUncommitted
	var states map[project.ProjectKey]*project.ProjectState
	if projectStateRequired {
		var err error
		states, err = project.GetProjectStates(jirix, projects, c.untracked || c.noUntracked || c.uncommitted || c.noUncommitted)
		if err != nil {
			return err
		}
	}
	mapInputs := map[project.ProjectKey]*mapInput{}
	var keys project.ProjectKeys
	for _, localProject := range projects {
		key := localProject.Key()
		if keysRE != nil {
			if !keysRE.MatchString(key.String()) {
				continue
			}
		}
		state := states[key]
		if branchRE != nil {
			found := false
			for _, br := range state.Branches {
				if branchRE.MatchString(br.Name) {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		if remoteRE != nil && !remoteRE.MatchString(localProject.Remote) {
			continue
		}
		if (c.untracked && !state.HasUntracked) || (c.noUntracked && state.HasUntracked) {
			continue
		}
		if (c.uncommitted && !state.HasUncommitted) || (c.noUncommitted && state.HasUncommitted) {
			continue
		}
		mapInputs[key] = &mapInput{
			Project: localProject,
			jirix:   jirix,
			key:     key,
		}
		keys = append(keys, key)
	}

	total := len(mapInputs)
	index := 1
	for _, mi := range mapInputs {
		mi.index = index
		mi.total = total
		index++
	}

	if c.topLevelFlags.DebugVerbose {
		fmt.Fprintf(jirix.Stdout(), "Project Names: %s\n", strings.Join(projectNames(mapInputs), " "))
		fmt.Fprintf(jirix.Stdout(), "Project Keys: %s\n", strings.Join(projectKeys(mapInputs), " "))
	}

	runner := &runner{
		jirix:          jirix,
		args:           args,
		interactive:    c.interactive,
		collateOutput:  c.collateOutput,
		exitOnError:    c.exitOnError,
		showNamePrefix: c.showNamePrefix,
		showKeyPrefix:  c.showKeyPrefix,
		showPathPrefix: c.showPathPrefix,
	}
	mr := simplemr.MR{}
	if c.interactive {
		// Run one mapper at a time.
		mr.NumMappers = 1
		sort.Sort(keys)
	} else {
		mr.NumMappers = int(jirix.Jobs)
	}
	in, out := make(chan *simplemr.Record, len(mapInputs)), make(chan *simplemr.Record, len(mapInputs))
	sigch := make(chan os.Signal, 1)
	signal.Notify(sigch, os.Interrupt)
	jirix.TimerPush("Map and Reduce")
	go func() { <-sigch; mr.Cancel() }()
	go mr.Run(in, out, runner, runner)
	for _, key := range keys {
		in <- &simplemr.Record{Key: key.String(), Values: []any{mapInputs[key]}}
	}
	close(in)
	<-out
	jirix.TimerPop()
	return mr.Error()
}
