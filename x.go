// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package jiri provides utilities used by the jiri tool and related tools.
package jiri

import (
	"context"
	"encoding/xml"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"go.fuchsia.dev/jiri/analytics_util"
	"go.fuchsia.dev/jiri/cmdline"
	"go.fuchsia.dev/jiri/color"
	"go.fuchsia.dev/jiri/envvar"
	"go.fuchsia.dev/jiri/log"
	"go.fuchsia.dev/jiri/timing"
	"go.fuchsia.dev/jiri/tool"
)

const (
	AttrsJSON          = "attributes.json"
	RootMetaDir        = ".jiri_root"
	ProjectMetaDir     = "jiri"
	ConfigFile         = "config"
	DefaultCacheSubdir = "cache"
	ProjectMetaFile    = "metadata.v2"
	ProjectConfigFile  = "config"
	JiriManifestFile   = ".jiri_manifest"

	// PreservePathEnv is the name of the environment variable that, when set to a
	// non-empty value, causes jiri tools to use the existing PATH variable,
	// rather than mutating it.
	PreservePathEnv = "JIRI_PRESERVE_PATH"

	// EnableSubmodulesMagicValue is the only accepted value of the
	// -enable-submodules flag to jiri init. "true" is no longer accepted, in
	// order to force checkouts where the value is "true" to switch back to
	// Jiri-managed projects. This value is not intended to be used by users,
	// only by unit tests.
	// TODO(fxbug.dev/386810791): Delete this once submodule support is fully
	// removed from Jiri.
	EnableSubmodulesMagicValue = "yes-please"
)

// Config represents jiri global config
type Config struct {
	CachePath         string   `xml:"cache>path,omitempty"`
	CipdParanoidMode  string   `xml:"cipd_paranoid_mode,omitempty"`
	CipdMaxThreads    int      `xml:"cipd_max_threads,omitempty"`
	Dissociate        bool     `xml:"cache>dissociate,omitempty"`
	Shared            bool     `xml:"cache>shared,omitempty"`
	RewriteSsoToHttps bool     `xml:"rewriteSsoToHttps,omitempty"`
	SsoCookiePath     string   `xml:"SsoCookiePath,omitempty"`
	LockfileEnabled   string   `xml:"lockfile>enabled,omitempty"`
	LockfileName      string   `xml:"lockfile>name,omitempty"`
	PrebuiltJSON      string   `xml:"prebuilt>JSON,omitempty"`
	FetchingAttrs     string   `xml:"fetchingAttrs,omitempty"`
	AnalyticsOptIn    string   `xml:"analytics>optin,omitempty"`
	AnalyticsUserId   string   `xml:"analytics>userId,omitempty"`
	Partial           bool     `xml:"partial,omitempty"`
	PartialSkip       []string `xml:"partialSkip,omitempty"`
	OffloadPackfiles  bool     `xml:"offloadPackfiles,omitempty"`
	// version user has opted-in to
	AnalyticsVersion string   `xml:"analytics>version,omitempty"`
	KeepGitHooks     bool     `xml:"keepGitHooks,omitempty"`
	EnableSubmodules string   `xml:"enableSubmodules,omitempty"`
	ExcludeDirs      []string `xml:"excludeDirs,omitempty"`

	XMLName struct{} `xml:"config"`
}

func (c *Config) Write(filename string) error {
	if c.CachePath != "" {
		var err error
		c.CachePath, err = cleanPath(c.CachePath)
		if err != nil {
			return err
		}
	}
	data, err := xml.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filename, data, 0644)
}

func ConfigFromFile(filename string) (*Config, error) {
	bytes, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	c := new(Config)
	if err := xml.Unmarshal(bytes, c); err != nil {
		return nil, err
	}
	return c, nil
}

func GitGetConfig(key string) (string, error) {
	cmd := exec.Command("git", "config", "--get", key)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	config := strings.Split(string(out), "\n")[0]
	return config, nil
}

// X holds the execution environment for the jiri tool and related tools.  This
// includes the jiri filesystem root directory.
//
// TODO(toddw): Other jiri state should be transitioned to this struct,
// including the manifest and related operations.
type X struct {
	*tool.Context
	Root                string
	Cwd                 string
	Usage               func(format string, args ...any) error
	config              *Config
	Cache               string
	CipdParanoidMode    bool
	CipdMaxThreads      int
	Dissociate          bool
	Shared              bool
	Jobs                uint
	KeepGitHooks        bool
	RewriteSsoToHttps   bool
	LockfileEnabled     bool
	LockfileName        string
	OffloadPackfiles    bool
	SsoCookiePath       string
	Partial             bool
	PartialSkip         []string
	PrebuiltJSON        string
	FetchingAttrs       string
	UsingSnapshot       bool
	UsingImportOverride bool
	OverrideOptional    bool
	IgnoreLockConflicts bool
	Color               color.Color
	Logger              *log.Logger
	failures            uint32
	Attempts            uint
	cleanupFuncs        []func()
	AnalyticsSession    *analytics_util.AnalyticsSession
	OverrideWarned      bool
	EnableSubmodules    bool
	ExcludeDirs         []string
}

func (jirix *X) IncrementFailures() {
	atomic.AddUint32(&jirix.failures, 1)
}

func (jirix *X) Failures() uint32 {
	return atomic.LoadUint32(&jirix.failures)
}

// This is not thread safe
func (jirix *X) AddCleanupFunc(cleanup func()) {
	jirix.cleanupFuncs = append(jirix.cleanupFuncs, cleanup)
}

// Executes all the cleanups added in LIFO order
func (jirix *X) RunCleanup() {
	for _, fn := range jirix.cleanupFuncs {
		// defer so that cleanups are executed in LIFO order
		defer fn()
	}
}

func (jirix *X) UsePartialClone(remote string) bool {
	if jirix.Partial {
		for _, r := range jirix.PartialSkip {
			if remote == r {
				return false
			}
		}
		return true
	}
	return false
}

type TopLevelFlags struct {
	Root               string
	Jobs               uint
	Color              string
	QuietVerbose       bool
	DebugVerbose       bool
	TraceVerbose       bool
	ShowProgress       bool
	ProgressWindowSize uint
	TimeLogThreshold   time.Duration
	DumpTiming         bool
	TimeFile           string
}

func (t *TopLevelFlags) SetFlags(f *flag.FlagSet) {
	f.StringVar(&t.Root, "root", "", "Jiri root directory")
	f.UintVar(&t.Jobs, "j", DefaultJobs, "Number of jobs (commands) to run simultaneously")
	f.StringVar(&t.Color, "color", "auto", "Use color to format output. Values can be always, never and auto")
	f.BoolVar(&t.ShowProgress, "show-progress", true, "Show progress.")
	f.UintVar(&t.ProgressWindowSize, "progress-window", 5, "Number of progress messages to show simultaneously. Should be between 1 and 10")
	f.DurationVar(&t.TimeLogThreshold, "time-log-threshold", time.Second*10, "Log time taken by operations if more than the passed value (eg 5s). This only works with -v and -vv.")
	f.BoolVar(&t.QuietVerbose, "quiet", false, "Only print user actionable messages")
	f.BoolVar(&t.QuietVerbose, "q", false, "Same as -quiet")
	f.BoolVar(&t.DebugVerbose, "v", false, "Print debug level output")
	f.BoolVar(&t.TraceVerbose, "vv", false, "Print trace level output")
	f.BoolVar(&t.DumpTiming, "time", false, "Dump timing information to stderr before exiting the program.")
	f.StringVar(&t.TimeFile, "timefile", "", "File to dump timing information to, if not stderr.")
}

var DefaultJobs = uint(runtime.NumCPU() * 2)

func init() {
	// Cap jobs at 50 to avoid flooding Gerrit with too many requests
	if DefaultJobs > 50 {
		DefaultJobs = 50
	}
}

func NewXFromContext(ctx context.Context, topLevelFlags TopLevelFlags) (*X, error) {
	return NewX(cmdline.EnvFromContext(ctx), topLevelFlags)
}

// NewX returns a new execution environment, given a cmdline env.
// It also prepends .jiri_root/bin to the PATH.
func NewX(env *cmdline.Env, flags TopLevelFlags) (*X, error) {
	cf := color.EnableColor(flags.Color)
	if cf != color.ColorAuto && cf != color.ColorAlways && cf != color.ColorNever {
		return nil, env.UsageErrorf("invalid value of -color flag")
	}
	color := color.NewColor(cf)

	loggerLevel := log.InfoLevel
	if flags.QuietVerbose {
		loggerLevel = log.WarningLevel
	} else if flags.TraceVerbose {
		loggerLevel = log.TraceLevel
	} else if flags.DebugVerbose {
		loggerLevel = log.DebugLevel
	}
	if flags.ProgressWindowSize < 1 {
		flags.ProgressWindowSize = 1
	} else if flags.ProgressWindowSize > 10 {
		flags.ProgressWindowSize = 10
	}
	logger := log.NewLogger(loggerLevel, color, flags.ShowProgress, flags.ProgressWindowSize, flags.TimeLogThreshold, env.Stdout, env.Stderr)

	ctx := tool.NewContextFromEnv(env)
	root, err := FindRoot(flags, ctx.Timer())
	if err != nil {
		return nil, err
	}

	if flags.Jobs == 0 {
		return nil, fmt.Errorf("No of concurrent jobs should be more than zero")
	}

	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	x := &X{
		Context:  ctx,
		Cwd:      cwd,
		Root:     root,
		Usage:    env.UsageErrorf,
		Jobs:     flags.Jobs,
		Color:    color,
		Logger:   logger,
		Attempts: 1,
	}
	configPath := filepath.Join(x.RootMetaDir(), ConfigFile)
	if _, err := os.Stat(configPath); err == nil {
		x.config, err = ConfigFromFile(configPath)
		if err != nil {
			return nil, err
		}
		// enableSubmodules=true is no longer respected due to the submodules
		// rollback.
		//
		// Only accept a special magic value in order to keep running unit tests
		// that validate the submodules rollback.
		// TODO(fxbug.dev/386810791): Delete this once submodule support is
		// fully removed from Jiri.
		if x.config.EnableSubmodules != "" && x.config.EnableSubmodules != EnableSubmodulesMagicValue {
			// Remove the enableSubmodules value from the config file to reflect
			// that it's no longer supported.
			x.config.EnableSubmodules = ""
			if err := x.config.Write(configPath); err != nil {
				return nil, err
			}
		}
	} else if os.IsNotExist(err) {
		x.config = &Config{}
	} else {
		return nil, err
	}
	if x.config != nil {
		x.KeepGitHooks = x.config.KeepGitHooks
		x.RewriteSsoToHttps = x.config.RewriteSsoToHttps
		x.SsoCookiePath = x.config.SsoCookiePath

		x.EnableSubmodules = x.config.EnableSubmodules == EnableSubmodulesMagicValue

		if x.config.LockfileEnabled == "" {
			x.LockfileEnabled = true
		} else {
			if val, err := strconv.ParseBool(x.config.LockfileEnabled); err != nil {
				return nil, fmt.Errorf("'config>lockfile>enable' flag should be true or false")
			} else {
				x.LockfileEnabled = val
			}
		}
		if x.config.CipdParanoidMode == "" {
			x.CipdParanoidMode = true
		} else {
			if val, err := strconv.ParseBool(x.config.CipdParanoidMode); err != nil {
				return nil, fmt.Errorf("'config>cipd_paranoid_mode' flag should be true or false")
			} else {
				x.CipdParanoidMode = val
			}
		}
		x.CipdMaxThreads = x.config.CipdMaxThreads
		x.LockfileName = x.config.LockfileName
		x.PrebuiltJSON = x.config.PrebuiltJSON
		x.FetchingAttrs = x.config.FetchingAttrs
		if x.LockfileName == "" {
			x.LockfileName = "jiri.lock"
		}
		if x.PrebuiltJSON == "" {
			x.PrebuiltJSON = "prebuilt.json"
		}
		x.Shared = x.config.Shared
		x.Partial = x.config.Partial
		x.PartialSkip = x.config.PartialSkip
		x.OffloadPackfiles = x.config.OffloadPackfiles
		x.Dissociate = x.config.Dissociate
		x.ExcludeDirs = x.config.ExcludeDirs
		if len(x.ExcludeDirs) == 0 && x.ExcludeDirs == nil {
			x.ExcludeDirs = append(x.ExcludeDirs, "out")
			x.ExcludeDirs = append(x.ExcludeDirs, "prebuilt")
		}
	}
	x.Cache, err = findCache(x.config)
	if err != nil {
		return nil, err
	}
	if ctx.Env()[PreservePathEnv] == "" {
		// Prepend .jiri_root/bin to the PATH, so execing a binary will
		// invoke the one in that directory, if it exists.  This is crucial for jiri
		// subcommands, where we want to invoke the binary that jiri installed, not
		// whatever is in the user's PATH.
		//
		// Note that we must modify the actual os env variable with os.SetEnv and
		// also the ctx.env, so that execing a binary through the os/exec package
		// and with ctx.Run both have the correct behavior.
		newPath := envvar.PrependUniqueToken(ctx.Env()["PATH"], string(os.PathListSeparator), x.BinDir())
		ctx.Env()["PATH"] = newPath
		if err := os.Setenv("PATH", newPath); err != nil {
			return nil, err
		}
	}
	setupAnalytics(x, env)
	x.AddCleanupFunc(func() {
		// When an error interrupts program execution, there may be lingering progress messages.
		// During cleanup, call the DisableProgress function to clear them in case they are present.
		x.Logger.DisableProgress()
	})
	return x, nil
}

func cleanPath(path string) (string, error) {
	result, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("EvalSymlinks(%v) failed: %v", path, err)
	}
	if !filepath.IsAbs(result) {
		return "", fmt.Errorf("%v isn't an absolute path", result)
	}
	return filepath.Clean(result), nil
}

func findCache(config *Config) (string, error) {
	// Use flag variable if set.
	if config != nil && config.CachePath != "" {
		return cleanPath(config.CachePath)
	}

	return "", nil
}

func FindRoot(flags TopLevelFlags, timer *timing.Timer) (string, error) {
	if timer != nil {
		timer.Push("find .jiri_root")
		defer timer.Pop()
	}

	if flags.Root != "" {
		return cleanPath(flags.Root)
	}

	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}

	path, err := filepath.Abs(wd)
	if err != nil {
		return "", err
	}

	paths := []string{path}
	for i := len(path) - 1; i >= 0; i-- {
		if os.IsPathSeparator(path[i]) {
			path = path[:i]
			if path == "" {
				path = "/"
			}
			paths = append(paths, path)
		}
	}

	for _, path := range paths {
		fi, err := os.Stat(filepath.Join(path, RootMetaDir))
		if err == nil && fi.IsDir() {
			return path, nil
		}
	}

	return "", fmt.Errorf("cannot find %v", RootMetaDir)
}

// Clone returns a clone of the environment.
func (x *X) Clone(opts tool.ContextOpts) *X {
	return &X{
		Context:           x.Context.Clone(opts),
		Root:              x.Root,
		Cwd:               x.Cwd,
		Usage:             x.Usage,
		Jobs:              x.Jobs,
		Cache:             x.Cache,
		Color:             x.Color,
		RewriteSsoToHttps: x.RewriteSsoToHttps,
		Logger:            x.Logger,
		failures:          x.failures,
		Attempts:          x.Attempts,
		cleanupFuncs:      x.cleanupFuncs,
		AnalyticsSession:  x.AnalyticsSession,
	}
}

// UsageErrorf prints the error message represented by the printf-style format
// and args, followed by the usage output.  The implementation typically calls
// cmdline.Env.UsageErrorf.
func (x *X) UsageErrorf(format string, args ...any) error {
	if x.Usage != nil {
		return x.Usage(format, args...)
	}
	return fmt.Errorf(format, args...)
}

// RootMetaDir returns the path to the root metadata directory.
func (x *X) RootMetaDir() string {
	return filepath.Join(x.Root, RootMetaDir)
}

// CIPDPath returns the path to directory containing cipd.
func (x *X) CIPDPath() string {
	return filepath.Join(x.RootMetaDir(), "bin", "cipd")
}

// JiriManifestFile returns the path to the .jiri_manifest file.
func (x *X) JiriManifestFile() string {
	return filepath.Join(x.Root, JiriManifestFile)
}

// BinDir returns the path to the bin directory.
func (x *X) BinDir() string {
	return filepath.Join(x.RootMetaDir(), "bin")
}

// ScriptsDir returns the path to the scripts directory.
func (x *X) ScriptsDir() string {
	return filepath.Join(x.RootMetaDir(), "scripts")
}

// SwapDir returns the path to the swap directory. This is used
// to stage moves of direcories that may be moving into subdirectories
// of themselves or to handle unnesting of repositories.
//
// We use a directory in `.jiri_root` to prevent the complexities of an
// arbitrary $TMPDIR which can cross mountpoints/filesystems.
func (x *X) SwapDir() string {
	return filepath.Join(x.RootMetaDir(), "swap")
}

// UpdateHistoryDir returns the path to the update history directory.
func (x *X) UpdateHistoryDir() string {
	return filepath.Join(x.RootMetaDir(), "update_history")
}

// UpdateHistoryLatestLink returns the path to a hard link that points to the
// latest update in the update history directory.
func (x *X) UpdateHistoryLatestLink() string {
	return filepath.Join(x.UpdateHistoryDir(), "latest")
}

// UpdateHistorySecondLatestLink returns the path to a hard link that points to
// the second latest update in the update history directory.
func (x *X) UpdateHistorySecondLatestLink() string {
	return filepath.Join(x.UpdateHistoryDir(), "second-latest")
}

// UpdateHistoryLogDir returns the path to the update history directory.
func (x *X) UpdateHistoryLogDir() string {
	return filepath.Join(x.RootMetaDir(), "update_history_log")
}

// UpdateHistoryLogLatestLink returns the path to a symlink that points to the
// latest update in the update history directory.
func (x *X) UpdateHistoryLogLatestLink() string {
	return filepath.Join(x.UpdateHistoryLogDir(), "latest")
}

// UpdateHistoryLogSecondLatestLink returns the path to a symlink that points to
// the second latest update in the update history directory.
func (x *X) UpdateHistoryLogSecondLatestLink() string {
	return filepath.Join(x.UpdateHistoryLogDir(), "second-latest")
}

func setupAnalytics(x *X, env *cmdline.Env) {
	enabledAnalytics := false
	var userID string
	analyticsCommandMsg := fmt.Sprintf("To check what data we collect run: %s\n"+
		"To opt-in run: %s\n"+
		"To opt-out run: %s",
		x.Color.Yellow("jiri init -show-analytics-data"),
		x.Color.Yellow("jiri init -analytics-opt=true %q", x.Root),
		x.Color.Yellow("jiri init -analytics-opt=false %q", x.Root))
	if x.config == nil || x.config.AnalyticsOptIn == "" {
		x.Logger.Warningf("Please opt in or out of analytics collection. You will receive this warning until an option is selected.\n%s\n\n", analyticsCommandMsg)
	} else if x.config.AnalyticsOptIn == "yes" {
		if x.config.AnalyticsUserId == "" || x.config.AnalyticsVersion == "" {
			x.Logger.Warningf("Please opt in or out of analytics collection. You will receive this warning until an option is selected.\n%s\n\n", analyticsCommandMsg)
		} else if x.config.AnalyticsVersion != analytics_util.Version {
			x.Logger.Warningf("You have opted in for old version of data collection. Please opt in/out again\n%s\n\n", analyticsCommandMsg)
		} else {
			userID = x.config.AnalyticsUserId
			enabledAnalytics = true
		}
	}
	as := analytics_util.NewAnalyticsSession(enabledAnalytics, "UA-101128147-1", userID)
	x.AnalyticsSession = as
	id := as.AddCommand(env.CommandName, env.CommandFlags)

	x.AddCleanupFunc(func() {
		x.Logger.Tracef("Finalizing analytics session (if enabled)")
		as.Done(id)
		as.SendAllAndWaitToFinish()
	})
}
