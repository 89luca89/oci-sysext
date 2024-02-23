/* SPDX-License-Identifier: GPL-3.0-only

This file is part of the oci-sysext project:
   https://github.com/89luca89/oci-sysext

Copyright (C) 2023 oci-sysext contributors

oci-sysext is free software; you can redistribute it and/or modify it
under the terms of the GNU General Public License version 3
as published by the Free Software Foundation.

oci-sysext is distributed in the hope that it will be useful, but
WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the GNU
General Public License for more details.

You should have received a copy of the GNU General Public License
along with oci-sysext; if not, see <http://www.gnu.org/licenses/>. */

// Package main is the main package, nothing much here, just:
//   - setup of the environment
//   - setup of cobra
package main

import (
	"log"
	"strings"

	"github.com/89luca89/oci-sysext/cmd"
	"github.com/spf13/cobra"
)

var version = "development"

func newApp() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:              "oci-sysext",
		Short:            "Manage containers and images",
		Version:          strings.TrimPrefix(version, "v"),
		SilenceUsage:     true,
		SilenceErrors:    true,
		TraverseChildren: true,
	}

	rootCmd.AddCommand(
		cmd.NewCreateCommand(),
		cmd.NewPullCommand(),
	)
	rootCmd.PersistentFlags().
		String("log-level", "", "log messages above specified level (debug, warn, warning, error)")

	return rootCmd
}

func main() {
	app := newApp()

	err := app.Execute()
	if err != nil {
		log.Fatalf("%+v\n", err)
	}
}
