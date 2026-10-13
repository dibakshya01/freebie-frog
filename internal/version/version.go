// Package version holds the build version of freebie-frog.
package version

// Version is the build version. Override at build time with:
//
//	go build -ldflags "-X github.com/dibakshya01/freebie-frog/internal/version.Version=v0.1.0"
var Version = "dev"
