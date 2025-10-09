// Copyright 2018 The Fuchsia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cipd

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.fuchsia.dev/jiri"
	"go.fuchsia.dev/jiri/cmdline"
	"go.fuchsia.dev/jiri/log"
	"go.fuchsia.dev/jiri/retry"
	"go.fuchsia.dev/jiri/version"
)

const (
	cipdBackend            = "https://chrome-infra-packages.appspot.com"
	exitCodeNoValidToken   = 1
	cipdManifestInvalidErr = cmdline.ErrExitCode(25)
)

var (
	// CipdPlatform represents the current runtime platform in cipd platform notation.
	CipdPlatform   Platform
	cipdOS         string
	cipdArch       string
	selfUpdateOnce sync.Once
	templateRE     = regexp.MustCompile(`\${[^}]*}`)

	// ErrSkipTemplate may be returned from Expander.Expand to indicate that
	// a given expansion doesn't apply to the current template parameters. For
	// example, expanding `"foo/${os=linux,mac}"` with a template parameter of "os"
	// == "win", would return ErrSkipTemplate.
	ErrSkipTemplate = errors.New("package template does not apply to the current system")

	//go:embed cipd_client_version
	untrimmedCipdVersion string
	//go:embed cipd_client_version.digests
	cipdVersionDigest string

	// cipdVersion is the pinned version of the CIPD CLI.
	//
	// Run `scripts/update_cipd.sh` to update the pin.
	//
	// Spaces must be trimmed because it's read from an embedded text file that
	// may contain trailing newlines.
	cipdVersion = strings.TrimSpace(untrimmedCipdVersion)

	// Matches legacy CIPD instance IDs
	hexMatcher = regexp.MustCompile("[a-fA-F0-9]{40}")

	// Matches allowed CIPD ref alphabet
	pkgRefMatcher = regexp.MustCompile(`^[a-z0-9_./\-]{1,256}$`)
)

func init() {
	cipdOS = runtime.GOOS
	cipdArch = runtime.GOARCH
	if cipdOS == "darwin" {
		cipdOS = "mac"
	}
	if cipdArch == "arm" {
		cipdArch = "armv6l"
	}
	CipdPlatform = Platform{cipdOS, cipdArch}
}

// FetchBinary downloads CIPD to the specified path.
func FetchBinary(jirix *jiri.X, binaryPath string) error {
	// Fetch cipd digest
	digest, _, err := fetchDigest(CipdPlatform.String())
	if err != nil {
		return err
	}
	return fetchBinaryImpl(jirix, binaryPath, CipdPlatform.String(), cipdVersion, digest)
}

func fetchBinaryImpl(jirix *jiri.X, binaryPath, platform, version, digest string) error {
	cipdURL := fmt.Sprintf("%s/client?platform=%s&version=%s", cipdBackend, platform, version)
	data, err := fetchFile(jirix, cipdURL)
	if err != nil {
		return err
	}
	if verified, err := verifyDigest(data, digest); err != nil || !verified {
		if err != nil {
			return err
		}
		return errors.New("cipd failed integrity test")
	}
	// cipd binary verified. Save to disk
	if _, err := os.Stat(filepath.Dir(binaryPath)); os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(binaryPath), 0755); err != nil {
			return fmt.Errorf("failed to create parent directory %q for cipd: %v", filepath.Dir(binaryPath), err)
		}
	}
	return writeFile(binaryPath, data)
}

// Bootstrap returns the path of a valid cipd binary. It will fetch cipd from
// remote if a valid cipd binary is not found. It will update cipd if there
// is a new version.
func Bootstrap(jirix *jiri.X) error {
	checkValidity := func() error {
		fileInfo, err := os.Stat(jirix.CIPDPath())
		if err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("cipd binary was not found at %q", jirix.CIPDPath())
			}
			return err
		}
		// Check if cipd binary has execution permission
		if fileInfo.Mode()&0111 == 0 {
			return fmt.Errorf("cipd binary at %q is not executable", jirix.CIPDPath())
		}
		return nil
	}

	if err := checkValidity(); err != nil {
		// Could not find cipd binary or cipd is invalid
		// Bootstrap it from scratch
		return FetchBinary(jirix, jirix.CIPDPath())
	}
	// cipd is found, do self update
	var e error
	selfUpdateOnce.Do(func() {
		e = selfUpdate(jirix.CIPDPath(), cipdVersion)
	})
	if e != nil {
		// Self update is unsuccessful, redo bootstrap
		if err := FetchBinary(jirix, jirix.CIPDPath()); err != nil {
			return err
		}
	}
	return nil
}

// FuchsiaPlatform returns a Platform struct which can be used in
// determining the correct path for prebuilt packages. It replace
// the os and arch names from cipd format to a format used by
// Fuchsia developers.
func FuchsiaPlatform(plat Platform) Platform {
	retPlat := Platform{
		OS:   plat.OS,
		Arch: plat.Arch,
	}
	// Currently cipd use "amd64" for x86_64 while fuchsia use "x64",
	// replace "amd64" with "x64".
	// There might be other differences that need to be addressed in
	// the future.
	switch retPlat.Arch {
	case "amd64":
		retPlat.Arch = "x64"
	}
	return retPlat
}

func fetchDigest(platform string) (digest, method string, err error) {
	var digestBuf bytes.Buffer
	digestBuf.Write([]byte(cipdVersionDigest))
	digestScanner := bufio.NewScanner(&digestBuf)
	for digestScanner.Scan() {
		curLine := digestScanner.Text()
		if len(curLine) == 0 || curLine[0] == '#' {
			// Skip comment or empty line
			continue
		}
		fields := strings.Fields(curLine)
		if len(fields) != 3 {
			return "", "", errors.New("unsupported cipd digest file format")
		}
		if fields[0] == platform {
			digest = fields[2]
			method = fields[1]
			err = nil
			return
		}
	}
	return "", "", errors.New("no matching platform found in cipd digest file")
}

func selfUpdate(cipdPath, cipdVersion string) error {
	args := []string{"selfupdate", "-version", cipdVersion, "-service-url", cipdBackend}
	command := exec.Command(cipdPath, args...)
	return command.Run()
}

func writeFile(filePath string, data []byte) error {
	tempFile, err := os.CreateTemp(path.Dir(filePath), "cipd.*")
	if err != nil {
		return err
	}
	defer tempFile.Close()
	defer os.Remove(tempFile.Name())
	if _, err := tempFile.Write(data); err != nil {
		// Write errors
		return errors.New("I/O error while downloading cipd binary")
	}
	// Set mode to rwxr-xr-x
	if err := tempFile.Chmod(0o755); err != nil {
		// Chmod errors
		return errors.New("I/O error while adding executable permission to cipd binary")
	}
	if err := tempFile.Close(); err != nil {
		return err
	}
	return os.Rename(tempFile.Name(), filePath)
}

func verifyDigest(data []byte, cipdDigest string) (bool, error) {
	hash := sha256.Sum256(data)
	hashString := fmt.Sprintf("%x", hash)
	if hashString == strings.ToLower(cipdDigest) {
		return true, nil
	}
	return false, nil
}

func getUserAgent() string {
	ua := "jiri/" + version.GitCommit
	if version.GitCommit == "" {
		ua += "debug"
	}
	return ua
}

func fetchFile(jirix *jiri.X, url string) ([]byte, error) {
	// Retry the fetch a hardcoded number of times. jirix.Attempts is intended
	// to only apply to Git operations, and Git operation retries may be
	// disabled even when HTTP file fetches should still be retried.
	const maxAttempts = 3

	client := &http.Client{}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", getUserAgent())
	var contents []byte
	if err := retry.Function(jirix, func() error {
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			return fmt.Errorf("got non-success response: %s", resp.Status)
		}
		contents, err = io.ReadAll(resp.Body)
		return err
	}, "bootstrapping cipd binary", retry.AttemptsOpt(maxAttempts)); err != nil {
		jirix.Logger.Errorf("error: failed to download cipd client: %v\n", err)
		return nil, err
	}
	return contents, nil
}

type packageACL struct {
	path   string
	access bool
}

func checkPackageACL(jirix *jiri.X, cipdPath, jsonDir string, c chan<- packageACL) {
	jsonFile, err := os.CreateTemp(jsonDir, "cipd*.json")
	if err != nil {
		jirix.Logger.Warningf("Error while creating temporary file for cipd")
		c <- packageACL{path: cipdPath, access: false}
		return
	}
	jsonFileName := jsonFile.Name()
	jsonFile.Close()

	args := []string{"acl-check", "-reader", "-json-output", jsonFileName, cipdPath}
	jirix.Logger.Debugf("Invoke cipd with %v", args)

	command := exec.Command(jirix.CIPDPath(), args...)
	var stdoutBuf, stderrBuf bytes.Buffer
	command.Stdout = &stdoutBuf
	command.Stderr = &stderrBuf
	// Return false if cipd cannot be executed or output jsonfile contains false.
	if err := command.Run(); err != nil {
		jirix.Logger.Debugf("Error while executing cipd, err: %q, stderr: %q", err, stderrBuf.String())
		c <- packageACL{path: cipdPath, access: false}
		return
	}

	jsonData, err := os.ReadFile(jsonFileName)
	if err != nil {
		c <- packageACL{path: cipdPath, access: false}
		return
	}

	var result struct {
		Result bool `json:"result"`
	}
	if err := json.Unmarshal(jsonData, &result); err != nil {
		c <- packageACL{path: cipdPath, access: false}
		return
	}

	if !result.Result {
		c <- packageACL{path: cipdPath, access: false}
		return
	}

	// Package can be accessed.
	c <- packageACL{path: cipdPath, access: true}
}

// CheckPackageACL checks cipd's access to packages in map "pkgs". The package
// names in "pkgs" should have trailing '/' removed before calling this
// function.
func CheckPackageACL(jirix *jiri.X, pkgs map[string]bool) error {
	// Not declared as CheckPackageACL(jirix *jiri.X, pkgs map[*package.Package]bool)
	// due to import cycles. Package jiri/package imports jiri/cipd so here we cannot
	// import jiri/package.
	if err := Bootstrap(jirix); err != nil {
		return err
	}

	jsonDir, err := os.MkdirTemp("", "jiri_cipd")
	if err != nil {
		return err
	}
	defer os.RemoveAll(jsonDir)

	// Create a sufficiently large channel such that a below
	// serial execution would not block.
	c := make(chan packageACL, len(pkgs))
	for key := range pkgs {
		if cipdOS == "mac" {
			// On Mac, check package ACLs serially.
			// See https://g-issues.fuchsia.dev/issues/42069083 for details.
			checkPackageACL(jirix, key, jsonDir, c)
		} else {
			// Check package ACLs in parallel.
			go checkPackageACL(jirix, key, jsonDir, c)
		}
	}

	for i := 0; i < len(pkgs); i++ {
		acl := <-c
		pkgs[acl.path] = acl.access
	}
	return nil
}

// CheckLoggedIn checks cipd's user login information. It will return true
// if login information is found or return false if login information is not
// found.
func CheckLoggedIn(jirix *jiri.X) (bool, error) {
	if err := Bootstrap(jirix); err != nil {
		return false, err
	}
	args := []string{"auth-info"}
	command := exec.Command(jirix.CIPDPath(), args...)
	var stdoutBuf, stderrBuf bytes.Buffer
	command.Stdout = &stdoutBuf
	command.Stderr = &stderrBuf
	if err := command.Run(); err != nil {
		stdErrMsg := strings.TrimSpace(stderrBuf.String())
		jirix.Logger.Debugf("Error happened while executing cipd, err: %q, stderr: %q", err, stdErrMsg)
		if err, ok := err.(*exec.ExitError); ok && err.ExitCode() == exitCodeNoValidToken {
			return false, nil
		}
		return false, fmt.Errorf("failed to check `cipd auth-info`: %w", err)
	}
	return true, nil
}

// Ensure runs cipd binary's ensure functionality over file. Fetched packages will be
// saved to projectRoot directory. Parameter timeout is in minutes.
func Ensure(jirix *jiri.X, file, projectRoot string, timeout uint) error {
	if err := Bootstrap(jirix); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Minute)
	defer cancel()
	args := []string{
		"ensure",
		"-ensure-file", file,
		"-root", projectRoot,
		"-max-threads", strconv.Itoa(jirix.CipdMaxThreads),
	}

	if jirix.Logger.LoggerLevel <= log.WarningLevel {
		// If jiri is running with -quiet, use cipd's "warning" log-level.
		args = append(args, "-log-level", "warning")
	} else if jirix.Logger.LoggerLevel >= log.DebugLevel {
		// If jiri is running with -v or louder, use cipd's "debug" log-level.
		args = append(args, "-log-level", "debug")
	}

	env := os.Environ()
	// Add User-Agent info for cipd
	env = append(env, "CIPD_HTTP_USER_AGENT_PREFIX="+getUserAgent())
	if !jirix.Logger.IsProgressEnabled() {
		// Force CIPD to use the simple UI (without progress bars) when Jiri
		// progress bars are disabled. See
		// https://chromium.googlesource.com/infra/luci/luci-go/+/59c5d4cd5499a35251cad9e0bcad659766f0e411/cipd/client/cli/main.go#70
		env = append(env, "CIPD_SIMPLE_TERMINAL_UI=1")
	}

	task := jirix.Logger.AddTaskMsg("Fetching CIPD packages")
	defer task.Done()
	jirix.Logger.Debugf("Invoke cipd with %v", args)

	// Construct arguments and invoke cipd for ensure file
	command := exec.CommandContext(ctx, jirix.CIPDPath(), args...)
	command.Env = env
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr

	err := command.Run()
	if ctx.Err() == context.DeadlineExceeded {
		err = ctx.Err()
	}
	return err
}

func EnsureFileVerify(jirix *jiri.X, file string) error {
	err := Bootstrap(jirix)
	if err != nil {
		return err
	}
	args := []string{
		"ensure-file-verify",
		"-ensure-file", file,
	}

	if (jirix.Logger.LoggerLevel <= log.WarningLevel) || (!jirix.Logger.IsProgressEnabled()) {
		// If jiri is running with -quiet or -show-progess=false, use cipd's "warning" log-level.
		args = append(args, "-log-level", "warning")
	} else if jirix.Logger.LoggerLevel >= log.DebugLevel {
		// If jiri is running with -v or louder, use cipd's "debug" log-level.
		args = append(args, "-log-level", "debug")
	}

	task := jirix.Logger.AddTaskMsg("Verifying CIPD ensure file")
	defer task.Done()
	jirix.Logger.Debugf("Invoke cipd with %v", args)

	// Construct arguments and invoke cipd for ensure file
	command := exec.Command(jirix.CIPDPath(), args...)
	var stdoutBuf, stderrBuf bytes.Buffer
	// Add User-Agent info for cipd
	command.Env = append(os.Environ(), "CIPD_HTTP_USER_AGENT_PREFIX="+getUserAgent())
	command.Stdin = os.Stdin
	// Redirect outputs since cipd will print verbose information even
	// if log-level is set to warning
	command.Stdout = &stdoutBuf
	command.Stderr = &stderrBuf

	if err := command.Run(); err != nil {
		jirix.Logger.Errorf("`cipd ensure-file-verify` failed: stdout: %s\nstderr: %s", stdoutBuf.String(), stderrBuf.String())
		return cipdManifestInvalidErr
	}

	return nil
}

// TODO: Using PackageLock in project package directly will cause an import
// cycle. Remove this type once we solve the this issue.

// PackageInstance describes package instance id information generated by cipd
// ensure-file-resolve. It is a copy of PackageLock type in project package.
type PackageInstance struct {
	PackageName string
	VersionTag  string
	InstanceID  string
}

// Resolve runs cipd binary's ensure-file-resolve functionality over file.
// It returns a slice containing resolved packages and cipd instance ids.
func Resolve(jirix *jiri.X, file string) ([]PackageInstance, error) {
	if err := Bootstrap(jirix); err != nil {
		return nil, err
	}
	args := []string{"ensure-file-resolve", "-ensure-file", file, "-log-level", "warning"}
	jirix.Logger.Debugf("Invoke cipd with %v", args)

	command := exec.Command(jirix.CIPDPath(), args...)
	command.Env = append(os.Environ(), "CIPD_HTTP_USER_AGENT_PREFIX="+getUserAgent())
	var stdoutBuf, stderrBuf bytes.Buffer
	command.Stdin = os.Stdin
	// Redirect outputs since cipd will print verbose information even
	// if log-level is set to warning
	command.Stdout = &stdoutBuf
	command.Stderr = &stderrBuf
	if err := command.Run(); err != nil {
		jirix.Logger.Errorf("cipd returned error: %v", stderrBuf.String())
		return nil, err
	}

	// cipd generates the version file in the same directory of the ensure file
	// if no error is returned
	versionFile := file[:len(file)-len(".ensure")] + ".version"
	defer os.Remove(versionFile)
	return parseVersions(versionFile)
}

func parseVersions(file string) ([]PackageInstance, error) {
	versionReader, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer versionReader.Close()
	versionScanner := bufio.NewScanner(versionReader)
	// An example cipd version looks like:
	// ==========================================================
	// # Do not modify manually. All changes will be overwritten.
	// fuchsia/clang/linux-amd64
	// 	git_revision:280fa3c2d2ddb0b5dcb31113c0b1c2259982b7e7
	// 	eRoGS8qgx370QAIRgLDmbhpdPey8ti47B2Z3LMzwcXQC
	//
	// fuchsia/clang/mac-amd64
	// 	git_revision:280fa3c2d2ddb0b5dcb31113c0b1c2259982b7e7
	// 	BQhlnpoWG081CyLzA0zB1vCr8YPdb2DO2jnYe3Lsw4oC
	// ===========================================================
	// Parse version file using DFA

	const (
		stWaitingPkg = "a package name"
		stWaitingVer = "a package version"
		stWaitingIID = "an instance ID"
		stWaitingNL  = "a new line"
	)

	state := stWaitingPkg
	pkg := ""
	ver := ""
	iid := ""
	lineNo := 0
	makeError := func(fmtStr string, args ...any) error {
		args = append([]any{lineNo}, args...)
		return fmt.Errorf("failed to parse versions file (line %d): "+fmtStr, args...)
	}
	output := make([]PackageInstance, 0)
	for versionScanner.Scan() {
		lineNo++
		line := strings.TrimSpace(versionScanner.Text())
		// Comments are grammatically insignificant (unlike empty lines), so skip
		// the completely.
		if len(line) > 0 && line[0] == '#' {
			continue
		}

		switch state {
		case stWaitingPkg:
			if line == "" {
				continue // can have more than one empty line between triples
			}
			pkg = line
			state = stWaitingVer

		case stWaitingVer:
			if line == "" {
				return nil, makeError("expecting a version name, not a new line")
			}
			ver = line
			state = stWaitingIID

		case stWaitingIID:
			if line == "" {
				return nil, makeError("expecting an instance ID, not a new line")
			}
			iid = line
			output = append(output, PackageInstance{pkg, ver, iid})
			pkg, ver, iid = "", "", ""
			state = stWaitingNL

		case stWaitingNL:
			if line == "" {
				state = stWaitingPkg
				continue
			}
			return nil, makeError("expecting an empty line between each version definition triple")
		}
	}
	return output, nil
}

// CheckFloatingRefs determines if pkgs contains a floating ref which shouldn't
// be used normally.
//
// Note that CIPD versions can either be:
//   - A legacy InstanceID (40 hex chars)
//   - A modern InstanceID (>28 chars, base64 encoded)
//   - A tag (contains ':')
//   - A ref
//
// Technically, there are additional semantic checks for modern InstanceID
// decoding (see
// https://pkg.go.dev/go.chromium.org/luci/cipd/common#ValidateInstanceID).
//
// However, for the purpose of this function, we only care about finding likely
// refs, so we use heuristics to approximate the fuller parsing without needing
// to invoke the cipd binary.
//
// A previous version of this code used `cipd describe` and then checked to see
// if the VersionTag was present in the Refs field of the returned JSON.
// Under the hood this makes an incredibly expensive RPC to CIPD and was
// dominating the database read costs there.
func CheckFloatingRefs(pkgs map[PackageInstance]bool) {
	for k := range pkgs {
		switch {
		case strings.Contains(k.VersionTag, ":"):
			// it's a tag, not floating
		case len(k.VersionTag) == 40 && hexMatcher.MatchString(k.VersionTag):
			// legacy InstanceID
		case !pkgRefMatcher.MatchString(k.VersionTag):
			// not a valid ref
		default:
			_, err := base64.RawURLEncoding.DecodeString(k.VersionTag)
			if err == nil {
				// we assume it's a modern InstanceID
			} else {
				// we assume it's a ref
				pkgs[k] = true
			}
		}
	}
}

// Platform contains the parameters for a "${platform}" template.
// The string value can be obtained by calling String().
type Platform struct {
	// OS defines the operating system of this platform. It can be any OS
	// supported by golang.
	OS string
	// Arch defines the CPU architecture of this platform. It can be any
	// architecture supported by golang.
	Arch string
}

// NewPlatform parses a platform string into Platform struct.
func NewPlatform(s string) (Platform, error) {
	fields := strings.Split(s, "-")
	if len(fields) != 2 {
		return Platform{"", ""}, fmt.Errorf("illegal platform %q", s)
	}
	return Platform{fields[0], fields[1]}, nil
}

// String generates a string represents the Platform in "OS"-"Arch" form.
func (p Platform) String() string {
	return p.OS + "-" + p.Arch
}

// Expander returns an Expander populated with p's fields.
func (p Platform) Expander() Expander {
	return Expander{
		"os":       p.OS,
		"arch":     p.Arch,
		"platform": p.String(),
	}
}

// Expander is a mapping of simple string substitutions which is used to
// expand cipd package name templates. For example:
//
//	ex, err := template.Expander{
//	  "platform": "mac-amd64"
//	}.Expand("foo/${platform}")
//
// `ex` would be "foo/mac-amd64".
type Expander map[string]string

// Expand applies package template expansion rules to the package template,
//
// If err == ErrSkipTemplate, that means that this template does not apply to
// this os/arch combination and should be skipped.
//
// The expansion rules are as follows:
//   - "some text" will pass through unchanged
//   - "${variable}" will directly substitute the given variable
//   - "${variable=val1,val2}" will substitute the given variable, if its value
//     matches one of the values in the list of values. If the current value
//     does not match, this returns ErrSkipTemplate.
//
// Attempting to expand an unknown variable is an error.
// After expansion, any lingering '$' in the template is an error.
func (t Expander) Expand(template string) (pkg string, err error) {
	skip := false

	pkg = templateRE.ReplaceAllStringFunc(template, func(parm string) string {
		// ${...}
		contents := parm[2 : len(parm)-1]

		varNameValues := strings.SplitN(contents, "=", 2)
		if len(varNameValues) == 1 {
			// ${varName}
			if value, ok := t[varNameValues[0]]; ok {
				return value
			}

			err = fmt.Errorf("unknown variable in ${%s}", contents)
		}

		// ${varName=value,value}
		ourValue, ok := t[varNameValues[0]]
		if !ok {
			err = fmt.Errorf("unknown variable %q", parm)
			return parm
		}

		for _, val := range strings.Split(varNameValues[1], ",") {
			if val == ourValue {
				return ourValue
			}
		}
		skip = true
		return parm
	})
	if skip {
		err = ErrSkipTemplate
	}
	if err == nil && strings.ContainsRune(pkg, '$') {
		err = fmt.Errorf("unable to process some variables in %q", template)
	}
	return
}

// Expand method expands a cipdPath that contains templates such as ${platform}
// into concrete full paths. It might return an empty slice if platforms
// do not match the requirements in cipdPath.
func Expand(cipdPath string, platforms []Platform) ([]string, error) {
	output := make([]string, 0)
	//expanders := make([]Expander, 0)
	if !MustExpand(cipdPath) {
		output = append(output, cipdPath)
		return output, nil
	}

	for _, plat := range platforms {
		pkg, err := plat.Expander().Expand(cipdPath)
		if err == ErrSkipTemplate {
			continue
		}
		if err != nil {
			return nil, err
		}
		output = append(output, pkg)
	}
	return output, nil
}

// Decl method expands a cipdPath that contains ${platform}, ${os}, ${arch}
// with information in platforms. Unlike the Expand method which
// returns a list of expanded cipd paths, the Decl method only returns a
// single path containing all platforms. For example, if platforms contain
// "linux-amd64" and "linux-arm64", ${platform} will be replaced to
// ${platform=linux-amd64,linux-arm64}. This is a workaround for a limitation
// in 'cipd ensure-file-resolve' which requires the header of '.ensure' file
// to contain all available platforms. But in some cases, a package may miss
// a particular platform, which will cause a crash on this cipd command. By
// explicitly list all supporting platforms in the cipdPath, we can avoid
// crashing cipd.
func Decl(cipdPath string, platforms []Platform) (string, error) {
	if !MustExpand(cipdPath) || len(platforms) == 0 {
		return cipdPath, nil
	}

	osMap := make(map[string]bool)
	platMap := make(map[string]bool)
	archMap := make(map[string]bool)

	replacedOS := "${os="
	replacedArch := "${arch="
	replacedPlat := "${platform="

	for _, plat := range platforms {
		if _, ok := osMap[plat.OS]; !ok {
			replacedOS += plat.OS + ","
			osMap[plat.OS] = true
		}
		if _, ok := archMap[plat.Arch]; !ok {
			replacedArch += plat.Arch + ","
			archMap[plat.Arch] = true
		}
		if _, ok := platMap[plat.String()]; !ok {
			replacedPlat += plat.String() + ","
			platMap[plat.String()] = true
		}
	}
	replacedOS = replacedOS[:len(replacedOS)-1] + "}"
	replacedArch = replacedArch[:len(replacedArch)-1] + "}"
	replacedPlat = replacedPlat[:len(replacedPlat)-1] + "}"

	cipdPath = strings.Replace(cipdPath, "${os}", replacedOS, -1)
	cipdPath = strings.Replace(cipdPath, "${arch}", replacedArch, -1)
	cipdPath = strings.Replace(cipdPath, "${platform}", replacedPlat, -1)
	return cipdPath, nil
}

// MustExpand checks if template usages such as "${platform}" exist
// in cipdPath. If they exist, this function will return true. Otherwise
// it returns false.
func MustExpand(cipdPath string) bool {
	return templateRE.MatchString(cipdPath)
}

// DefaultPlatforms returns a slice of Platform objects that are currently
// validated by jiri.
func DefaultPlatforms() []Platform {
	return []Platform{
		{"linux", "amd64"},
		{"mac", "amd64"},
	}
}
