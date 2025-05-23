# Jiri Manifest

Jiri manifest files describe the set of projects that get synced when running "jiri update".

The first manifest file that jiri reads is in [root]/.jiri\_manifest.  This root manifest
**must** exist for the jiri tool to work.

Usually the manifest in [root]/.jiri\_manifest will import other manifests from remote repositories via &lt;import> tags, but it can contain its own list of projects as well.

Manifests have the following XML schema:
```
<manifest>
  <imports>
    <import remote="https://vanadium.googlesource.com/manifest"
            manifest="public"
            name="manifest"
    />
    <localimport file="/path/to/local/manifest"/>
    ...
  </imports>
  <projects>
    <project name="my-project"
             path="path/where/project/lives"
             protocol="git"
             remote="https://github.com/myorg/foo"
             revision="ed42c05d8688ab23"
             remotebranch="my-branch"
             gerrithost="https://myorg-review.googlesource.com"
             githooks="path/to/githooks-dir"
             gitsubmodules="true"
    />
    ...
  </projects>
  <packages>
    <package name="package/path"
             version="version"
             path="path/to/directory"
             internal="true"
             platforms="os-arch1,os-arch2..."
             attributes="attr1,attr2,..."
             flag="filename|content_successful|content_failed"
    />
    ...
  </packages>
  <overrides>
    <project ... />
  </overrides>
  <hooks>
    <hook name="update"
          project="mojo/public"
          action="update.sh"/>
    ...
  </hooks>

</manifest>
```
The &lt;import> and &lt;localimport> tags can be used to share common projects across multiple manifests.

A &lt;localimport> tag should be used when the manifest being imported and the importing manifest are both in the same repository, or when neither one is in a repository.  The "file" attribute is the path to the
manifest file being imported.  It can be absolute, or relative to the importing manifest file.

If the manifest being imported and the importing manifest are in different repositories then an &lt;import> tag must be used, with the following attributes:

* remote (required) - The remote url of the repository containing the manifest to be imported

* manifest (required) - The path of the manifest file to be imported, relative to the repository root.

* name (optional) - The name of the project corresponding to the manifest repository.  If your manifest contains a &lt;project> with the same remote as the manifest remote, then the "name" attribute of on the
&lt;import> tag should match the "name" attribute on the &lt;project>.  Otherwise, jiri will clone the manifest repository on every update.

The &lt;project> tags describe the projects to sync, and what state they should sync to, according to the following attributes:

* name (required) - The name of the project.

* path (required) - The location where the project will be located, relative to the jiri root.

* remote (required) - The remote url of the project repository.

* protocol (optional) - The protocol to use when cloning and syncing the repo. Currently "git" is the default and only supported protocol.

* remotebranch (optional) - The remote branch that the project will sync to. Defaults to "main".  The "remotebranch" attribute is ignored if "revision" is specified.

* revision (optional) - The specific revision (usually a git SHA) that the project will sync to.  If "revision" is  specified then the "remotebranch" attribute is ignored.

* gerrithost (optional) - The url of the Gerrit host for the project.  If specified, then running "jiri cl upload" will upload a CL to this Gerrit host.

* githooks (optional) - The path (relative to the jiri root) of a directory containing git hooks that will be installed in the projects .git/hooks directory during each update.

* gitsubmodules (optional) - Whether the project has git submodules (https://git-scm.com/book/en/v2/Git-Tools-Submodules), this attribute needs to be set to `true`. By default it is `false`.

* gitsubmoduleof (optional) - The superproject that the project is a part of when submodules are enabled. If specified and the superproject enabled for submodules, jiri will delete the project from the tree and add it as a submodule. By default it is empty.

The &lt;packages> tags describe the CIPD packages to sync, and what version they should sync to, according to the following attributes:

* name (required) - The CIPD path of the package.

* version (required) - The version tag of the CIPD package. Floating refs are not recommended.

* path (optional) - The local path this package should be stored. It should be a relative path based on `JIRI_ROOT`. If the manifest does not define this attribute, it will be put into `JIRI_ROOT/prebuilt` directory. Jiri allows the path to be platform specific, for example `path="buildtools/{{.OS}}-{{.Arch}}"`, if run jiri under linux-amd64, it will be expanded to `path="buildtools/linux-amd64"`

* internal (optional) - Whether the package is accessible to the public. If a cipd package requires explicit permissions such as packages under fuchsia_internal, this attribute needs to be set to `true`. By default it is `false`.

* platforms (optional) - The platforms supported by the package. By default, it is set to `linux-amd64,mac-amd64`. However, if this package supports other platforms, e.g. `linux-arm64`, this attribute needs to be explicitly defined.

* attributes (optional) - If this is set for a package, it will not be fetched by default. These packages can be included by setting optional attributes using `jiri init -fetch-optional=attr1,attr2`.

* flag (optional) - The flag needs to be written by jiri when this package is successfully fetched. The flag attribute has a format of `filename|content_successful|content_failed` When a package is successfully downloaded, jiri will write `content_succeful` to filename. If the package is not downloaded due to access reasons, jiri will write `content_failed` to filename.

The projects in the &lt;overrides> tag replace existing projects defined by in the &lt;projects> tag (and from transitively imported &lt;projects> tags).
Only the root manifest can contain overrides and repositories referenced using the
&lt;import> tag (including from transitive imports) cannot be overridden.

The &lt;hook> tag describes the hooks that must be executed after every 'jiri update' They are configured via the following attributes:

* name (required) - The name of the of the hook to identify it

* project (required) - The name of the project where the hook is present

* action (required) - Action to be performed inside the project. It is mostly identified by a script
