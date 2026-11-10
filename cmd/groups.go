package cmd

import (
	"fmt"

	"github.com/joshskidmore/cloudtail/config"
	"github.com/spf13/cobra"
)

// TODO: colorize based on logGroup prefix (/aws/lambda, /aws/kinesisfirehose, etc...)
var groupsConfig config.Configuration

var groupsCommand = &cobra.Command{
	Use:   "groups",
	Short: "List log groups",
	Long:  "",
	Run: func(cmd *cobra.Command, args []string) {
		b := newClient(cmd.Context(), &groupsConfig, nil)
		logGroups, err := b.GetLogGroups(cmd.Context())
		if err != nil {
			fatal(err)
		}
		for _, group := range logGroups {
			fmt.Println(*group.LogGroupName)
		}
	},
}

func init() {
	groupsCommand.Flags().StringVar(&groupsConfig.Prefix, "prefix", "", "log group prefix filter")
}
