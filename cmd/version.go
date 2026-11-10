package cmd

import (
	"fmt"
	"runtime/debug"
	"strings"

	"github.com/spf13/cobra"
)

// version is set by the release build (see .goreleaser.yaml). Without it,
// buildVersion uses the module version that `go install` records.
var version = ""

func buildVersion() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return strings.TrimPrefix(info.Main.Version, "v")
	}
	return "dev"
}

var versionCommand = &cobra.Command{
	Use:   "version",
	Short: "Prints the version string",
	Long:  "",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("v%s\n", buildVersion())
	},
}
