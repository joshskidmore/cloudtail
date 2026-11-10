package main

import (
	"os"

	"github.com/joshskidmore/cloudtail/cmd"
)

func main() {
	if err := cmd.RootCommand.Execute(); err != nil {
		os.Exit(1)
	}
}
