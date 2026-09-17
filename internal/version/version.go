// Package version declares the CERTOOL release, so every report can say which
// build produced its verdicts.
package version

// Version is the declared release. Bump it with every published release.
const Version = "0.2.0"

// String is the name/version form printed by --version and in reports.
func String() string { return "certool/" + Version }
