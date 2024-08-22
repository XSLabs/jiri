// Copyright 2016 The Fuchsia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package subcommands

import (
	"context"
	"crypto/rand"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"github.com/google/subcommands"
	"go.fuchsia.dev/jiri"
	"go.fuchsia.dev/jiri/analytics_util"
	"go.fuchsia.dev/jiri/cmdline"
)

const (
	optionalAttrsNotSet = "[ATTRIBUTES_NOT_SET]"
)

// TODO(https://fxbug.dev/356134056): delete when finished migrating to
// subcommands library.
var (
	initFlags initCmd
	cmdInit   = &cmdline.Command{
		Name:   initFlags.Name(),
		Short:  initFlags.Synopsis(),
		Long:   initFlags.Usage(),
		Runner: cmdline.RunnerFunc(initFlags.run),
	}
)

// TODO(https://fxbug.dev/356134056): delete when finished migrating to
// subcommands library.
func init() {
	initFlags.SetFlags(&cmdInit.Flags)
}

type initCmd struct {
	cmdBase

	cache                           string
	dissociate                      bool
	shared                          bool
	showAnalyticsData               bool
	analyticsOpt                    string
	rewriteSsoToHttps               string
	ssoCookie                       string
	keepGitHooks                    string
	enableLockfile                  string
	lockfileName                    string
	prebuiltJSON                    string
	enableSubmodules                string
	forceDisableSubmodulesInfraOnly string
	optionalAttrs                   string
	partial                         bool
	partialSkip                     arrayFlag
	offloadPackfiles                bool
	cipdParanoid                    string
	cipdMaxThreads                  int
	excludeDirs                     arrayFlag
}

func (c *initCmd) Name() string     { return "init" }
func (c *initCmd) Synopsis() string { return "Create a new jiri root" }
func (c *initCmd) Usage() string {
	return `The "init" command creates new jiri "root" - basically a [root]/.jiri_root
directory and template files.

Running "init" in existing jiri [root] is safe.

Usage:
  jiri init [flags] [directory]

If you provide a directory, the command is run inside it. If this directory
does not exists, it will be created.
`
}

func (c *initCmd) SetFlags(f *flag.FlagSet) {
	f.StringVar(&c.cache, "cache", "", "Jiri cache directory.")
	f.BoolVar(&c.shared, "shared", false, "[DEPRECATED] All caches are shared.")
	f.BoolVar(&c.dissociate, "dissociate", false, "Dissociate the git cache after a clone or fetch.")
	f.BoolVar(&c.showAnalyticsData, "show-analytics-data", false, "Show analytics data that jiri collect when you opt-in and exits.")
	f.StringVar(&c.analyticsOpt, "analytics-opt", "", "Opt in/out of analytics collection. Takes true/false")
	f.StringVar(&c.rewriteSsoToHttps, "rewrite-sso-to-https", "", "Rewrites sso fetches, clones, etc to https. Takes true/false.")
	f.StringVar(&c.ssoCookie, "sso-cookie-path", "", "Path to master SSO cookie file.")
	f.StringVar(&c.keepGitHooks, "keep-git-hooks", "", "Whether to keep current git hooks in '.git/hooks' when doing 'jiri update'. Takes true/false.")
	f.StringVar(&c.enableLockfile, "enable-lockfile", "", "Enable lockfile enforcement")
	f.StringVar(&c.lockfileName, "lockfile-name", "", "Set up filename of lockfile")
	f.StringVar(&c.prebuiltJSON, "prebuilt-json", "", "Set up filename for prebuilt json file")
	f.StringVar(&c.enableSubmodules, "enable-submodules", "", "Enable submodules structure")
	// Used to forcibly roll out submodules to users while still allowing infra to opt out.
	f.StringVar(&c.forceDisableSubmodulesInfraOnly, "force-disable-submodules-infra-only", "", "Force disable submodules.")
	// Empty string is not used as default value for optionalAttrs as we
	// use empty string to clear existing saved attributes.
	f.StringVar(&c.optionalAttrs, "fetch-optional", optionalAttrsNotSet, "Set up attributes of optional projects and packages that should be fetched by jiri.")
	f.BoolVar(&c.partial, "partial", false, "Whether to use a partial checkout.")
	f.Var(&c.partialSkip, "skip-partial", "Skip using partial checkouts for these remotes.")
	f.BoolVar(&c.offloadPackfiles, "offload-packfiles", true, "Whether to use a CDN for packfiles if available.")
	f.StringVar(&c.cipdParanoid, "cipd-paranoid-mode", "", "Whether to use paranoid mode in cipd.")
	// Default (0) causes CIPD to use as many threads as there are CPUs.
	f.IntVar(&c.cipdMaxThreads, "cipd-max-threads", 0, "Number of threads to use for unpacking CIPD packages. If zero, uses all CPUs.")
	f.Var(&c.excludeDirs, "exclude-dirs", "Directories to skip when searching for local projects (Default: out).")
}

func (c *initCmd) Execute(ctx context.Context, f *flag.FlagSet, _ ...any) subcommands.ExitStatus {
	return errToExitStatus(ctx, c.run(ctx, f.Args()))
}

func (c *initCmd) run(ctx context.Context, args []string) error {
	if len(args) > 1 {
		return fmt.Errorf("wrong number of arguments")
	}

	if c.showAnalyticsData {
		fmt.Printf("%s\n", analytics_util.CollectedData)
		return nil
	}

	var dir string
	var err error
	if len(args) == 1 {
		dir, err = filepath.Abs(args[0])
		if err != nil {
			return err
		}
		if _, err := os.Stat(dir); err != nil {
			if !os.IsNotExist(err) {
				return err
			}
			if err := os.Mkdir(dir, 0755); err != nil {
				return err
			}
		}
	} else {
		dir, err = os.Getwd()
		if err != nil {
			return err
		}
	}

	d := filepath.Join(dir, jiri.RootMetaDir)
	if _, err := os.Stat(d); err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		if err := os.Mkdir(d, 0755); err != nil {
			return err
		}
	}

	if c.cache != "" {
		cache, err := filepath.Abs(c.cache)
		if err != nil {
			return err
		}
		if _, err := os.Stat(cache); os.IsNotExist(err) {
			if err := os.MkdirAll(cache, 0755); err != nil {
				return err
			}
		}
	}

	config := &jiri.Config{}
	configPath := filepath.Join(d, jiri.ConfigFile)
	if _, err := os.Stat(configPath); err == nil {
		config, err = jiri.ConfigFromFile(configPath)
		if err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	if c.cache != "" {
		config.CachePath = c.cache
	}

	if c.dissociate {
		config.Dissociate = true
	}

	if c.keepGitHooks != "" {
		if val, err := strconv.ParseBool(c.keepGitHooks); err != nil {
			return fmt.Errorf("'keep-git-hooks' c. should be true or false")
		} else {
			config.KeepGitHooks = val
		}
	}

	if gitConfigSubm, err := jiri.GitGetConfig("jiri.enableSubmodules"); err == nil {
		if _, err := strconv.ParseBool(gitConfigSubm); err != nil {
			return fmt.Errorf("'jiri.enableSubmodules' from git config should be true or false")
		}
		config.EnableSubmodules = gitConfigSubm
	}

	if c.enableSubmodules != "" {
		if _, err := strconv.ParseBool(c.enableSubmodules); err != nil {
			return fmt.Errorf("'enable-submodules' c. should be true or false")
		} else {
			config.EnableSubmodules = c.enableSubmodules
		}
	}

	// Override EnableSubmodules values with False if ForceDisableSubmodulesInfraOnly c. is True
	if c.forceDisableSubmodulesInfraOnly != "" {
		if val, err := strconv.ParseBool(c.forceDisableSubmodulesInfraOnly); err != nil {
			return fmt.Errorf("'force-disable-submodules-infra-only' c. should be true or false")
		} else {
			if val {
				config.ForceDisableSubmodulesInfraOnly = "true"
			}
		}
	}

	if c.rewriteSsoToHttps != "" {
		if val, err := strconv.ParseBool(c.rewriteSsoToHttps); err != nil {
			return fmt.Errorf("'rewrite-sso-to-https' c. should be true or false")
		} else {
			config.RewriteSsoToHttps = val
		}
	}

	if c.optionalAttrs != optionalAttrsNotSet {
		config.FetchingAttrs = c.optionalAttrs
	}

	if c.partial {
		config.Partial = c.partial
	}

	for _, r := range c.partialSkip {
		config.PartialSkip = append(config.PartialSkip, r)
	}

	if c.offloadPackfiles {
		config.OffloadPackfiles = c.offloadPackfiles
	}

	if c.ssoCookie != "" {
		config.SsoCookiePath = c.ssoCookie
	}

	if c.lockfileName != "" {
		config.LockfileName = c.lockfileName
	}

	if c.prebuiltJSON != "" {
		config.PrebuiltJSON = c.prebuiltJSON
	}

	if c.enableLockfile != "" {
		if _, err := strconv.ParseBool(c.enableLockfile); err != nil {
			return fmt.Errorf("'enable-lockfile' c. should be true or false")
		}
		config.LockfileEnabled = c.enableLockfile
	}

	if c.cipdParanoid != "" {
		if _, err := strconv.ParseBool(c.cipdParanoid); err != nil {
			return fmt.Errorf("'cipd-paranoid-mode' c. should be true or false")
		}
		config.CipdParanoidMode = c.cipdParanoid
	}

	config.CipdMaxThreads = c.cipdMaxThreads

	if c.analyticsOpt != "" {
		if val, err := strconv.ParseBool(c.analyticsOpt); err != nil {
			return fmt.Errorf("'analytics-opt' c. should be true or false")
		} else {
			if val {
				config.AnalyticsOptIn = "yes"
				config.AnalyticsVersion = analytics_util.Version

				bytes := make([]byte, 16)
				io.ReadFull(rand.Reader, bytes)
				if err != nil {
					return err
				}
				bytes[6] = (bytes[6] & 0x0f) | 0x40
				bytes[8] = (bytes[8] & 0x3f) | 0x80

				config.AnalyticsUserId = fmt.Sprintf("%x-%x-%x-%x-%x", bytes[0:4], bytes[4:6], bytes[6:8], bytes[8:10], bytes[10:])
			} else {
				config.AnalyticsOptIn = "no"
				config.AnalyticsVersion = ""
				config.AnalyticsUserId = ""
			}
		}
	}

	if len(c.excludeDirs) != 0 {
		config.ExcludeDirs = []string{}
	}

	for _, r := range c.excludeDirs {
		config.ExcludeDirs = append(config.ExcludeDirs, r)
	}

	if err := config.Write(configPath); err != nil {
		return err
	}
	// TODO(phosek): also create an empty manifest

	return nil
}
