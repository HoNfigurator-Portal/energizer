// Package dashboard provides the embedded dashboard UI assets.
// The dist/ directory is populated by running "npm run build" in the dashboard/ folder.
// When dist/ exists at build time, all its files are compiled into the Go binary.
package dashboard

import "embed"

// DistFS holds the embedded dashboard/dist files.
// If dashboard/dist does not exist at build time, the embed will be empty
// and the dashboard will not be available.
//
//go:embed all:dist
var DistFS embed.FS
