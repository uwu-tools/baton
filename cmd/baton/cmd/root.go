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

// Package cmd holds baton's command tree.
package cmd

import (
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"sigs.k8s.io/release-utils/log"
	"sigs.k8s.io/release-utils/version"

	"github.com/uwu-tools/baton/internal/github"
	"github.com/uwu-tools/baton/pkg/config"
	"github.com/uwu-tools/baton/pkg/probe"
	"github.com/uwu-tools/baton/pkg/source"
	"github.com/uwu-tools/baton/pkg/writer"
)

// ErrEvaluation means the run finished but some obligation, API call,
// or inventory check failed. main exits 2 for it, 1 for anything else.
var ErrEvaluation = errors.New("evaluation failed")

// Clients are the GitHub-backed APIs a command uses.
type Clients struct {
	Source source.Source
	Issues writer.Issues
}

// ClientFactory builds Clients on first use, so offline commands such
// as validate never need a token.
type ClientFactory func() (*Clients, error)

// GitHubClients is the production ClientFactory.
func GitHubClients() (*Clients, error) {
	token, err := github.Token()
	if err != nil {
		return nil, fmt.Errorf("github token: %w", err)
	}

	c, err := github.New(token)
	if err != nil {
		return nil, fmt.Errorf("github client: %w", err)
	}

	return &Clients{Source: c, Issues: c}, nil
}

// New returns the baton root command.
func New(clients ClientFactory, now func() time.Time) *cobra.Command {
	var logLevel string

	root := &cobra.Command{
		Use:   "baton",
		Short: "Track the work one component owes another",
		Long: "baton reads a baton file, checks which follow-up work each component owes,\n" +
			"and keeps issues open until the work is verified done.",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(*cobra.Command, []string) error {
			if err := log.SetupGlobalLogger(logLevel); err != nil {
				return fmt.Errorf("set up logging: %w", err)
			}

			return nil
		},
	}
	root.PersistentFlags().StringVar(&logLevel, "log-level", "info",
		"the logging verbosity, either "+log.LevelNames())
	root.AddCommand(
		newValidate(),
		newPlan(clients, now),
		newApply(clients, now),
		version.WithFont("doom"),
	)

	return root
}

// Execute runs the root command with production dependencies.
func Execute() error {
	if err := New(GitHubClients, time.Now).Execute(); err != nil {
		return fmt.Errorf("baton: %w", err)
	}

	return nil
}

func loadConfig(path string) (*config.Config, error) {
	cfg, err := config.Load(path, probe.Known)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	return cfg, nil
}

// printer keeps the first write error so callers check it once.
type printer struct {
	w   io.Writer
	err error
}

func (p *printer) f(format string, a ...any) {
	if p.err == nil {
		_, p.err = fmt.Fprintf(p.w, format, a...)
	}
}
