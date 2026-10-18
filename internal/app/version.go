package app

import "runtime/debug"

// Release builds set these with -ldflags -X. Local source runs stay identifiable
// as development builds instead of claiming a published application version.
var (
	version  = "dev"
	revision = ""
)

func buildRevision() string {
	if revision != "" {
		return revision
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	commit, modified := "unknown", false
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			commit = setting.Value
		case "vcs.modified":
			modified = setting.Value == "true"
		}
	}
	if modified {
		commit += "-dirty"
	}
	return commit
}
