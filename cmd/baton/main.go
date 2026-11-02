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

// Command baton tracks the work one component owes another.
//
// Exit status: 0 on success, 1 on bad usage or config, 2 if any
// obligation, API call, or inventory check failed.
package main

import (
	"errors"
	"os"

	"github.com/sirupsen/logrus"

	"github.com/uwu-tools/baton/cmd/baton/cmd"
)

func main() {
	err := cmd.Execute()
	if err == nil {
		return
	}

	logrus.Error(err)

	if errors.Is(err, cmd.ErrEvaluation) {
		os.Exit(2)
	}

	os.Exit(1)
}
