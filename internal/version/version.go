// Package version contains build version and release metadata.
package version

import "runtime"

var Version = "dev"
var Commit = "unknown"
var Date = "unknown"

const Repository = "mtch3n/trellis"

func AssetName() string { return "trellis_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz" }
