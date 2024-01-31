// Copyright 2016 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

//go:build none
// +build none

package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"

	"github.com/ava-labs/coreth/common"
	"github.com/ava-labs/coreth/internal/build"
)

var (
	// Files that end up in the geth*.zip archive.
	gethArchiveFiles = []string{
		"COPYING",
		executablePath("geth"),
	}

	// Files that end up in the geth-alltools*.zip archive.
	allToolsArchiveFiles = []string{
		"COPYING",
		executablePath("abigen"),
		executablePath("bootnode"),
		executablePath("evm"),
		executablePath("geth"),
		executablePath("rlpdump"),
		executablePath("clef"),
	}

	// Distros for which packages are created.
	// Note: vivid is unsupported because there is no golang-1.6 package for it.
	// Note: the following Ubuntu releases have been officially deprecated on Launchpad:
	//   wily, yakkety, zesty, artful, cosmic, disco, eoan, groovy, hirsuite, impish
	debDistroGoBoots = map[string]string{
		"trusty":  "golang-1.11", // EOL: 04/2024
		"xenial":  "golang-go",   // EOL: 04/2026
		"bionic":  "golang-go",   // EOL: 04/2028
		"focal":   "golang-go",   // EOL: 04/2030
		"jammy":   "golang-go",   // EOL: 04/2032
		"kinetic": "golang-go",   // EOL: 07/2023
		"lunar":   "golang-go",   // EOL: 01/2024
	}

	debGoBootPaths = map[string]string{
		"golang-1.11": "/usr/lib/go-1.11",
		"golang-go":   "/usr/lib/go",
	}

	// This is the version of Go that will be downloaded by
	//
	//     go run ci.go install -dlgo
	dlgoVersion = "1.20.7"

	// This is the version of Go that will be used to bootstrap the PPA builder.
	//
	// This version is fine to be old and full of security holes, we just use it
	// to build the latest Go. Don't change it. If it ever becomes insufficient,
	// we need to switch over to a recursive builder to jumpt across supported
	// versions.
	gobootVersion = "1.19.6"
)

var GOBIN, _ = filepath.Abs(filepath.Join("build", "bin"))

func executablePath(name string) string {
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(GOBIN, name)
}

func main() {
	log.SetFlags(log.Lshortfile)

	if !common.FileExist(filepath.Join("build", "ci.go")) {
		log.Fatal("this script must be run from the root of the repository")
	}
	if len(os.Args) < 2 {
		log.Fatal("need subcommand as first argument")
	}
	switch os.Args[1] {
	case "test":
		doTest(os.Args[2:])
	case "lint":
		doLint(os.Args[2:])
	default:
		log.Fatal("unknown command ", os.Args[1])
	}
}

// Running The Tests
//
// "tests" also includes static analysis tools such as vet.

func doTest(cmdline []string) {
	var (
		dlgo     = flag.Bool("dlgo", false, "Download Go and build with it")
		arch     = flag.String("arch", "", "Run tests for given architecture")
		cc       = flag.String("cc", "", "Sets C compiler binary")
		coverage = flag.Bool("coverage", false, "Whether to record code coverage")
		verbose  = flag.Bool("v", false, "Whether to log verbosely")
		race     = flag.Bool("race", false, "Execute the race detector")
	)
	flag.CommandLine.Parse(cmdline)

	// Configure the toolchain.
	tc := build.GoToolchain{GOARCH: *arch, CC: *cc}
	if *dlgo {
		csdb := build.MustLoadChecksums("build/checksums.txt")
		tc.Root = build.DownloadGo(csdb, dlgoVersion)
	}
	gotest := tc.Go("test")

	// CI needs a bit more time for the statetests (default 10m).
	gotest.Args = append(gotest.Args, "-timeout=20m")

	// Enable CKZG backend in CI.
	gotest.Args = append(gotest.Args, "-tags=ckzg")

	// Test a single package at a time. CI builders are slow
	// and some tests run into timeouts under load.
	gotest.Args = append(gotest.Args, "-p", "1")
	if *coverage {
		gotest.Args = append(gotest.Args, "-covermode=atomic", "-cover")
	}
	if *verbose {
		gotest.Args = append(gotest.Args, "-v")
	}
	if *race {
		gotest.Args = append(gotest.Args, "-race")
	}

	packages := []string{"./..."}
	if len(flag.CommandLine.Args()) > 0 {
		packages = flag.CommandLine.Args()
	}
	gotest.Args = append(gotest.Args, packages...)
	build.MustRun(gotest)
}

// doLint runs golangci-lint on requested packages.
func doLint(cmdline []string) {
	var (
		cachedir = flag.String("cachedir", "./build/cache", "directory for caching golangci-lint binary.")
	)
	flag.CommandLine.Parse(cmdline)
	packages := []string{"./..."}
	if len(flag.CommandLine.Args()) > 0 {
		packages = flag.CommandLine.Args()
	}

	linter := downloadLinter(*cachedir)
	lflags := []string{"run", "--config", ".golangci.yml"}
	build.MustRunCommand(linter, append(lflags, packages...)...)
	fmt.Println("You have achieved perfection.")
}

// downloadLinter downloads and unpacks golangci-lint.
func downloadLinter(cachedir string) string {
	const version = "1.55.2"

	csdb := build.MustLoadChecksums("build/checksums.txt")
	arch := runtime.GOARCH
	ext := ".tar.gz"

	if runtime.GOOS == "windows" {
		ext = ".zip"
	}
	if arch == "arm" {
		arch += "v" + os.Getenv("GOARM")
	}
	base := fmt.Sprintf("golangci-lint-%s-%s-%s", version, runtime.GOOS, arch)
	url := fmt.Sprintf("https://github.com/golangci/golangci-lint/releases/download/v%s/%s%s", version, base, ext)
	archivePath := filepath.Join(cachedir, base+ext)
	if err := csdb.DownloadFile(url, archivePath); err != nil {
		log.Fatal(err)
	}
	if err := build.ExtractArchive(archivePath, cachedir); err != nil {
		log.Fatal(err)
	}
	return filepath.Join(cachedir, base, "golangci-lint")
}
