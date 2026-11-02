//go:build mage

/*
Copyright 2026 The uwu-tools Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"fmt"
	"path/filepath"

	"github.com/magefile/mage/sh"
	"github.com/uwu-tools/magex/pkg"
	"sigs.k8s.io/release-utils/mage"
)

// Default target to run when none is specified.
var Default = Verify

const (
	binDir              = "bin"
	scriptDir           = "scripts"
	golangciLintVersion = "v2.14.0"
)

var boilerplateDir = filepath.Join(scriptDir, "boilerplate")

// All runs every verification and the unit tests.
func All() error {
	if err := Verify(); err != nil {
		return err
	}
	return Test()
}

// Test runs the unit tests.
func Test() error {
	return mage.TestGo(true)
}

// E2ETest replays the Scorecard baton file against live GitHub.
func E2ETest() error {
	return mage.TestGoWithTags(true, "e2e", "test/e2e")
}

// Generate regenerates the counterfeiter fakes.
func Generate() error {
	return sh.RunV("go", "generate", "./...")
}

// Verify runs the repository verification checks.
func Verify() error {
	fmt.Println("Ensuring mage is available...")
	if err := pkg.EnsureMage(""); err != nil {
		return err
	}

	fmt.Println("Running copyright header checks...")
	if err := mage.VerifyBoilerplate("v0.2.5", binDir, boilerplateDir, false); err != nil {
		return err
	}

	fmt.Println("Running go module linter...")
	if err := mage.VerifyGoMod(); err != nil {
		return err
	}

	fmt.Println("Running golangci-lint...")
	return mage.RunGolangCILint(golangciLintVersion, false)
}

// Clean removes build output.
func Clean() error {
	fmt.Println("Cleaning workspace...")
	return sh.Rm(binDir)
}
