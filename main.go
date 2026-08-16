// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

// Package main is the entry point for the skog application.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/uraniumdawn/skog/pkg/ui"
)

// Build-time variables set via ldflags
var (
	version = "0.0.999"
	commit  = "?"
	date    = ""
)

func main() {
	versionFlag := flag.Bool("version", false, "Print version information and exit")
	flag.Parse()

	if *versionFlag {
		fmt.Printf("skog %s (commit: %s, built: %s)\n", version, commit, date)
		os.Exit(0)
	}

	ui.Version = version
	app := ui.NewApp()
	app.Run()
}
