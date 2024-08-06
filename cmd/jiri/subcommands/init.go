// Copyright 2016 The Fuchsia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package subcommands

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"go.fuchsia.dev/jiri"
	"go.fuchsia.dev/jiri/analytics_util"
	"go.fuchsia.dev/jiri/cmdline"
)

var cmdInit = &cmdline.Command{
	Runner: cmdline.RunnerFunc(runInit),
	Name:   "init",
	Short:  "Create a new jiri root",
	Long: `
The "init" command creates new jiri "root" - basically a [root]/.jiri_root
directory and template files.

Running "init" in existing jiri [root] is safe.
`,
	ArgsName: "[directory]",
	ArgsLong: `
If you provide a directory, the command is run inside it. If this directory
does not exists, it will be created.
`,
}

var initFlags struct {
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

const (
	optionalAttrsNotSet = "[ATTRIBUTES_NOT_SET]"
)

func init() {
	cmdInit.Flags.StringVar(&initFlags.cache, "cache", "", "Jiri cache directory.")
	cmdInit.Flags.BoolVar(&initFlags.shared, "shared", false, "[DEPRECATED] All caches are shared.")
	cmdInit.Flags.BoolVar(&initFlags.dissociate, "dissociate", false, "Dissociate the git cache after a clone or fetch.")
	cmdInit.Flags.BoolVar(&initFlags.showAnalyticsData, "show-analytics-data", false, "Show analytics data that jiri collect when you opt-in and exits.")
	cmdInit.Flags.StringVar(&initFlags.analyticsOpt, "analytics-opt", "", "Opt in/out of analytics collection. Takes true/false")
	cmdInit.Flags.StringVar(&initFlags.rewriteSsoToHttps, "rewrite-sso-to-https", "", "Rewrites sso fetches, clones, etc to https. Takes true/false.")
	cmdInit.Flags.StringVar(&initFlags.ssoCookie, "sso-cookie-path", "", "Path to master SSO cookie file.")
	cmdInit.Flags.StringVar(&initFlags.keepGitHooks, "keep-git-hooks", "", "Whether to keep current git hooks in '.git/hooks' when doing 'jiri update'. Takes true/false.")
	cmdInit.Flags.StringVar(&initFlags.enableLockfile, "enable-lockfile", "", "Enable lockfile enforcement")
	cmdInit.Flags.StringVar(&initFlags.lockfileName, "lockfile-name", "", "Set up filename of lockfile")
	cmdInit.Flags.StringVar(&initFlags.prebuiltJSON, "prebuilt-json", "", "Set up filename for prebuilt json file")
	cmdInit.Flags.StringVar(&initFlags.enableSubmodules, "enable-submodules", "", "Enable submodules structure")
	// This initFlags. will be used to forcibly roll out submodules to users while still allowing infra to opt out.
	cmdInit.Flags.StringVar(&initFlags.forceDisableSubmodulesInfraOnly, "force-disable-submodules-infra-only", "", "Force disable submodules.")
	// Empty string is not used as default value for optionalAttrs as we
	// use empty string to clear existing saved attributes.
	cmdInit.Flags.StringVar(&initFlags.optionalAttrs, "fetch-optional", optionalAttrsNotSet, "Set up attributes of optional projects and packages that should be fetched by jiri.")
	cmdInit.Flags.BoolVar(&initFlags.partial, "partial", false, "Whether to use a partial checkout.")
	cmdInit.Flags.Var(&initFlags.partialSkip, "skip-partial", "Skip using partial checkouts for these remotes.")
	cmdInit.Flags.BoolVar(&initFlags.offloadPackfiles, "offload-packfiles", true, "Whether to use a CDN for packfiles if available.")
	cmdInit.Flags.StringVar(&initFlags.cipdParanoid, "cipd-paranoid-mode", "", "Whether to use paranoid mode in cipd.")
	// Default (0) causes CIPD to use as many threads as there are CPUs.
	cmdInit.Flags.IntVar(&initFlags.cipdMaxThreads, "cipd-max-threads", 0, "Number of threads to use for unpacking CIPD packages. If zero, uses all CPUs.")
	cmdInit.Flags.Var(&initFlags.excludeDirs, "exclude-dirs", "Directories to skip when searching for local projects (Default: out).")
}

func runInit(ctx context.Context, args []string) error {
	if len(args) > 1 {
		return fmt.Errorf("wrong number of arguments")
	}

	if initFlags.showAnalyticsData {
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
		dir, err = jiri.GetCwd()
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

	if initFlags.cache != "" {
		cache, err := filepath.Abs(initFlags.cache)
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

	if initFlags.cache != "" {
		config.CachePath = initFlags.cache
	}

	if initFlags.dissociate {
		config.Dissociate = true
	}

	if initFlags.keepGitHooks != "" {
		if val, err := strconv.ParseBool(initFlags.keepGitHooks); err != nil {
			return fmt.Errorf("'keep-git-hooks' initFlags. should be true or false")
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

	if initFlags.enableSubmodules != "" {
		if _, err := strconv.ParseBool(initFlags.enableSubmodules); err != nil {
			return fmt.Errorf("'enable-submodules' initFlags. should be true or false")
		} else {
			config.EnableSubmodules = initFlags.enableSubmodules
		}
	}

	// Override EnableSubmodules values with False if ForceDisableSubmodulesInfraOnly initFlags. is True
	if initFlags.forceDisableSubmodulesInfraOnly != "" {
		if val, err := strconv.ParseBool(initFlags.forceDisableSubmodulesInfraOnly); err != nil {
			return fmt.Errorf("'force-disable-submodules-infra-only' initFlags. should be true or false")
		} else {
			if val {
				config.ForceDisableSubmodulesInfraOnly = "true"
			}
		}
	}

	if initFlags.rewriteSsoToHttps != "" {
		if val, err := strconv.ParseBool(initFlags.rewriteSsoToHttps); err != nil {
			return fmt.Errorf("'rewrite-sso-to-https' initFlags. should be true or false")
		} else {
			config.RewriteSsoToHttps = val
		}
	}

	if initFlags.optionalAttrs != optionalAttrsNotSet {
		config.FetchingAttrs = initFlags.optionalAttrs
	}

	if initFlags.partial {
		config.Partial = initFlags.partial
	}

	for _, r := range initFlags.partialSkip {
		config.PartialSkip = append(config.PartialSkip, r)
	}

	if initFlags.offloadPackfiles {
		config.OffloadPackfiles = initFlags.offloadPackfiles
	}

	if initFlags.ssoCookie != "" {
		config.SsoCookiePath = initFlags.ssoCookie
	}

	if initFlags.lockfileName != "" {
		config.LockfileName = initFlags.lockfileName
	}

	if initFlags.prebuiltJSON != "" {
		config.PrebuiltJSON = initFlags.prebuiltJSON
	}

	if initFlags.enableLockfile != "" {
		if _, err := strconv.ParseBool(initFlags.enableLockfile); err != nil {
			return fmt.Errorf("'enable-lockfile' initFlags. should be true or false")
		}
		config.LockfileEnabled = initFlags.enableLockfile
	}

	if initFlags.cipdParanoid != "" {
		if _, err := strconv.ParseBool(initFlags.cipdParanoid); err != nil {
			return fmt.Errorf("'cipd-paranoid-mode' initFlags. should be true or false")
		}
		config.CipdParanoidMode = initFlags.cipdParanoid
	}

	config.CipdMaxThreads = initFlags.cipdMaxThreads

	if initFlags.analyticsOpt != "" {
		if val, err := strconv.ParseBool(initFlags.analyticsOpt); err != nil {
			return fmt.Errorf("'analytics-opt' initFlags. should be true or false")
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

	if len(initFlags.excludeDirs) != 0 {
		config.ExcludeDirs = []string{}
	}

	for _, r := range initFlags.excludeDirs {
		config.ExcludeDirs = append(config.ExcludeDirs, r)
	}

	if err := config.Write(configPath); err != nil {
		return err
	}
	// TODO(phosek): also create an empty manifest

	return nil
}
