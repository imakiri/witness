//go:build mage
// +build mage

// Mage build script for the witness Grafana data source plugin. Inherits
// every target (build, buildAll, clean, lint, ...) from the official
// grafana-plugin-sdk-go build module — invoke `mage -l` to list them.
package main

import (
	// mage:import
	build "github.com/grafana/grafana-plugin-sdk-go/build"
)

// Default target used when `mage` is called with no arguments.
var Default = build.BuildAll
