// Copyright 2019 The Fuchsia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package subcommands

import (
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestUpdateVersion(t *testing.T) {
	t.Parallel()

	manifestContent := `
<?xml version="1.0" encoding="UTF-8"?>
<manifest>
<packages>
    <!-- Packages for buildtools -->
    <!--   GN -->
    <package name="gn/gn/${platform}"
             version="git_revision:89d5ef56cb999a1cb007b2671d375932703d4665"
             path="buildtools/{{.OS}}-{{.Arch}}"/>
    <!--   Ninja -->
    <package name="infra/ninja/${platform}"
             version="git_revision:9eac2058b70615519b2c4d8c6bdbfca1bd079e39"
             path="buildtools/{{.OS}}-{{.Arch}}"/>
    <!--   Breakpad -->
    <package name="fuchsia/tools/breakpad/${os=linux}-${arch}"
             version="git_revision:9eac2058b70615519b2c4d8c6bdbfca1bd079e39"
             path="buildtools/{{.OS}}-{{.Arch}}"/>
</packages>
</manifest>
`

	tests := make(map[*packageChanges]string)
	tests[&packageChanges{
		Name:   "gn/gn/${platform}",
		OldVer: "git_revision:89d5ef56cb999a1cb007b2671d375932703d4665",
		NewVer: "git_revision:ffffffffffffffffffffffffffffffffffffffff",
	}] = `
<?xml version="1.0" encoding="UTF-8"?>
<manifest>
<packages>
    <!-- Packages for buildtools -->
    <!--   GN -->
    <package name="gn/gn/${platform}"
             version="git_revision:ffffffffffffffffffffffffffffffffffffffff"
             path="buildtools/{{.OS}}-{{.Arch}}"/>
    <!--   Ninja -->
    <package name="infra/ninja/${platform}"
             version="git_revision:9eac2058b70615519b2c4d8c6bdbfca1bd079e39"
             path="buildtools/{{.OS}}-{{.Arch}}"/>
    <!--   Breakpad -->
    <package name="fuchsia/tools/breakpad/${os=linux}-${arch}"
             version="git_revision:9eac2058b70615519b2c4d8c6bdbfca1bd079e39"
             path="buildtools/{{.OS}}-{{.Arch}}"/>
</packages>
</manifest>
`
	tests[&packageChanges{
		Name:   "fuchsia/tools/breakpad/${os=linux}-${arch}",
		OldVer: "git_revision:9eac2058b70615519b2c4d8c6bdbfca1bd079e39",
		NewVer: "git_revision:ffffffffffffffffffffffffffffffffffffffff",
	}] = `
<?xml version="1.0" encoding="UTF-8"?>
<manifest>
<packages>
    <!-- Packages for buildtools -->
    <!--   GN -->
    <package name="gn/gn/${platform}"
             version="git_revision:89d5ef56cb999a1cb007b2671d375932703d4665"
             path="buildtools/{{.OS}}-{{.Arch}}"/>
    <!--   Ninja -->
    <package name="infra/ninja/${platform}"
             version="git_revision:9eac2058b70615519b2c4d8c6bdbfca1bd079e39"
             path="buildtools/{{.OS}}-{{.Arch}}"/>
    <!--   Breakpad -->
    <package name="fuchsia/tools/breakpad/${os=linux}-${arch}"
             version="git_revision:ffffffffffffffffffffffffffffffffffffffff"
             path="buildtools/{{.OS}}-{{.Arch}}"/>
</packages>
</manifest>
`

	for k, v := range tests {
		if res, err := updateVersion(manifestContent, "package", *k); err != nil {
			t.Errorf("test updateVersion failed due to error: %v", err)
		} else if res != v {
			t.Errorf("expect:%s\n got:%s\nwhen testing updateVersion", v, res)
		}
	}
}

func TestUpdateRevision(t *testing.T) {
	t.Parallel()

	mainifestContent := `
<?xml version="1.0" encoding="UTF-8"?>
<!-- See README.dart.md for instructions on how to update this -->
<manifest>
	<projects>
	<project name="dart/sdk"
				path="third_party/dart"
				remote="https://dart.googlesource.com/sdk"
				gerrithost="https://dart-review.googlesource.com"
				revision="224f82c21cb2966f36ab850eae7ef5c8697cc477"/>
	<project name="dart/observatory_pub_packages"
				path="third_party/dart/third_party/observatory_pub_packages"
				remote="https://dart.googlesource.com/observatory_pub_packages/"
				gerrithost="https://dart-review.googlesource.com"
				revision="0894122173b0f98eb08863a7712e78407d4477bc"/>

	<project name="third_party/dart-pkg"
				path="third_party/dart-pkg/pub"
				remote="https://fuchsia.googlesource.com/third_party/dart-pkg"
				gerrithost="https://fuchsia-review.googlesource.com"/>
	</projects>
</manifest>
`

	tests := make(map[*projectChanges]string)
	tests[&projectChanges{
		Name:   "dart/sdk",
		Remote: "",
		Path:   "",
		OldRev: "224f82c21cb2966f36ab850eae7ef5c8697cc477",
		NewRev: "ffffffffffffffffffffffffffffffffffffffff",
	}] = `
<?xml version="1.0" encoding="UTF-8"?>
<!-- See README.dart.md for instructions on how to update this -->
<manifest>
	<projects>
	<project name="dart/sdk"
				path="third_party/dart"
				remote="https://dart.googlesource.com/sdk"
				gerrithost="https://dart-review.googlesource.com"
				revision="ffffffffffffffffffffffffffffffffffffffff"/>
	<project name="dart/observatory_pub_packages"
				path="third_party/dart/third_party/observatory_pub_packages"
				remote="https://dart.googlesource.com/observatory_pub_packages/"
				gerrithost="https://dart-review.googlesource.com"
				revision="0894122173b0f98eb08863a7712e78407d4477bc"/>

	<project name="third_party/dart-pkg"
				path="third_party/dart-pkg/pub"
				remote="https://fuchsia.googlesource.com/third_party/dart-pkg"
				gerrithost="https://fuchsia-review.googlesource.com"/>
	</projects>
</manifest>
`

	tests[&projectChanges{
		Name:   "third_party/dart-pkg",
		Remote: "",
		Path:   "",
		OldRev: "",
		NewRev: "ffffffffffffffffffffffffffffffffffffffff",
	}] = `
<?xml version="1.0" encoding="UTF-8"?>
<!-- See README.dart.md for instructions on how to update this -->
<manifest>
	<projects>
	<project name="dart/sdk"
				path="third_party/dart"
				remote="https://dart.googlesource.com/sdk"
				gerrithost="https://dart-review.googlesource.com"
				revision="224f82c21cb2966f36ab850eae7ef5c8697cc477"/>
	<project name="dart/observatory_pub_packages"
				path="third_party/dart/third_party/observatory_pub_packages"
				remote="https://dart.googlesource.com/observatory_pub_packages/"
				gerrithost="https://dart-review.googlesource.com"
				revision="0894122173b0f98eb08863a7712e78407d4477bc"/>

	<project name="third_party/dart-pkg"
				path="third_party/dart-pkg/pub"
				remote="https://fuchsia.googlesource.com/third_party/dart-pkg"
				gerrithost="https://fuchsia-review.googlesource.com"
         revision="ffffffffffffffffffffffffffffffffffffffff"/>
	</projects>
</manifest>
`
	for k, v := range tests {
		if res, err := updateRevision(mainifestContent, "project", k.OldRev, k.NewRev, k.Name); err != nil {
			t.Errorf("test updateRevision failed due to error: %v", err)
		} else if res != v {
			t.Errorf("expect:%s\n got:%s\nwhen testing updateVersion", v, res)
		}
	}
}

func TestUpdateDuplicateRevision(t *testing.T) {
	t.Parallel()

	manifestContent := `
<?xml version="1.0" encoding="UTF-8"?>
<manifest>
  <projects>
    <!-- The ICU library to use for top-of-tree Fuchsia builds -->
    <project name="chromium/deps/icu@default"
             gitsubmoduleof="fuchsia"
             path="third_party/icu/default"
             remote="https://chromium.googlesource.com/chromium/deps/icu"
             gerrithost="https://chromium-review.googlesource.com"
             revision="1b7d391f0528fb3a4976b7541b387ee04f915f83"/>
    <!-- The ICU library to use for "stable" Fuchsia builds -->
    <project name="chromium/deps/icu@stable"
             gitsubmoduleof="fuchsia"
             path="third_party/icu/stable"
             remote="https://chromium.googlesource.com/chromium/deps/icu"
             gerrithost="https://chromium-review.googlesource.com"
             revision="da07448619763d1cde255b361324242646f5b268"/>
    <!-- The ICU library to use for "latest" Fuchsia builds -->
    <project name="chromium/deps/icu@latest"
             gitsubmoduleof="fuchsia"
             path="third_party/icu/latest"
             remote="https://chromium.googlesource.com/chromium/deps/icu"
             gerrithost="https://chromium-review.googlesource.com"
             revision="da07448619763d1cde255b361324242646f5b268"/>
    <project name="chromium/deps/icu@extra"
             gitsubmoduleof="fuchsia"
             path="third_party/icu/latest"
             remote="https://chromium.googlesource.com/chromium/deps/icu"
             gerrithost="https://chromium-review.googlesource.com"/>
  </projects>
</manifest>
`

	tests := make(map[*projectChanges]string)
	tests[&projectChanges{
		Name:   "chromium/deps/icu@latest",
		Remote: "",
		Path:   "",
		OldRev: "da07448619763d1cde255b361324242646f5b268",
		NewRev: "1b7d391f0528fb3a4976b7541b387ee04f915f83",
	}] = `
<?xml version="1.0" encoding="UTF-8"?>
<manifest>
  <projects>
    <!-- The ICU library to use for top-of-tree Fuchsia builds -->
    <project name="chromium/deps/icu@default"
             gitsubmoduleof="fuchsia"
             path="third_party/icu/default"
             remote="https://chromium.googlesource.com/chromium/deps/icu"
             gerrithost="https://chromium-review.googlesource.com"
             revision="1b7d391f0528fb3a4976b7541b387ee04f915f83"/>
    <!-- The ICU library to use for "stable" Fuchsia builds -->
    <project name="chromium/deps/icu@stable"
             gitsubmoduleof="fuchsia"
             path="third_party/icu/stable"
             remote="https://chromium.googlesource.com/chromium/deps/icu"
             gerrithost="https://chromium-review.googlesource.com"
             revision="da07448619763d1cde255b361324242646f5b268"/>
    <!-- The ICU library to use for "latest" Fuchsia builds -->
    <project name="chromium/deps/icu@latest"
             gitsubmoduleof="fuchsia"
             path="third_party/icu/latest"
             remote="https://chromium.googlesource.com/chromium/deps/icu"
             gerrithost="https://chromium-review.googlesource.com"
             revision="1b7d391f0528fb3a4976b7541b387ee04f915f83"/>
    <project name="chromium/deps/icu@extra"
             gitsubmoduleof="fuchsia"
             path="third_party/icu/latest"
             remote="https://chromium.googlesource.com/chromium/deps/icu"
             gerrithost="https://chromium-review.googlesource.com"/>
  </projects>
</manifest>
`
	tests[&projectChanges{
		Name:   "chromium/deps/icu@extra",
		Remote: "",
		Path:   "",
		OldRev: "",
		NewRev: "1b7d391f0528fb3a4976b7541b387ee04f915f83",
	}] = `
<?xml version="1.0" encoding="UTF-8"?>
<manifest>
  <projects>
    <!-- The ICU library to use for top-of-tree Fuchsia builds -->
    <project name="chromium/deps/icu@default"
             gitsubmoduleof="fuchsia"
             path="third_party/icu/default"
             remote="https://chromium.googlesource.com/chromium/deps/icu"
             gerrithost="https://chromium-review.googlesource.com"
             revision="1b7d391f0528fb3a4976b7541b387ee04f915f83"/>
    <!-- The ICU library to use for "stable" Fuchsia builds -->
    <project name="chromium/deps/icu@stable"
             gitsubmoduleof="fuchsia"
             path="third_party/icu/stable"
             remote="https://chromium.googlesource.com/chromium/deps/icu"
             gerrithost="https://chromium-review.googlesource.com"
             revision="da07448619763d1cde255b361324242646f5b268"/>
    <!-- The ICU library to use for "latest" Fuchsia builds -->
    <project name="chromium/deps/icu@latest"
             gitsubmoduleof="fuchsia"
             path="third_party/icu/latest"
             remote="https://chromium.googlesource.com/chromium/deps/icu"
             gerrithost="https://chromium-review.googlesource.com"
             revision="da07448619763d1cde255b361324242646f5b268"/>
    <project name="chromium/deps/icu@extra"
             gitsubmoduleof="fuchsia"
             path="third_party/icu/latest"
             remote="https://chromium.googlesource.com/chromium/deps/icu"
             gerrithost="https://chromium-review.googlesource.com"
             revision="1b7d391f0528fb3a4976b7541b387ee04f915f83"/>
  </projects>
</manifest>
`

	for k, v := range tests {
		k := k
		v := v
		t.Run(fmt.Sprintf("%v", k), func(t *testing.T) {
			t.Parallel()
			if res, err := updateRevision(manifestContent, "project", k.OldRev, k.NewRev, k.Name); err != nil {
				t.Errorf("test updateRevision failed due to error: %v", err)
			} else if res != v {
				t.Errorf("expect:%s\n got:%s\nwhen testing updateVersion, diff:\n%v", v, res, cmp.Diff(v, res))
			}
		})
	}
}
