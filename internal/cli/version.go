package cli

// Version is replaced at build time with:
// -ldflags="-X numa-perfman/internal/cli.Version=<version>"
var Version = "dev"

func versionString() string {
	if Version != "" {
		return Version
	}
	return "dev"
}
