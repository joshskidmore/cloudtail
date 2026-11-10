package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/joshskidmore/cloudtail/cloudwatch"
	"github.com/joshskidmore/cloudtail/config"
	"github.com/spf13/cobra"
)

// RootCommand is the main top-level command
var RootCommand = &cobra.Command{
	Use:   "cloudtail [command]",
	Short: "Terminal viewer for AWS CloudWatch Logs",
	Long:  "cloudtail is a terminal viewer for AWS CloudWatch Logs: pick log groups, then tail, search, and filter them.",
	Example: `  cloudtail                        # pick log groups and watch them
  cloudtail watch production api   # watch two log groups
  cloudtail groups
  cloudtail streams production
  cloudtail get production --start -2h`,
	Run: func(cmd *cobra.Command, args []string) {
		runWatch(nil)
	},
}

var awsConfig config.AWSConfiguration

// newClient creates the AWS service client and exits with an error if that fails.
func newClient(ctx context.Context, cfg *config.Configuration, output *config.OutputConfiguration) *cloudwatch.Client {
	svc, err := cloudwatch.NewService(ctx, &awsConfig)
	if err != nil {
		fatal(err)
	}
	return cloudwatch.NewClient(svc, cfg, output)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "Error:", err)
	os.Exit(1)
}

func init() {
	RootCommand.AddCommand(groupsCommand)
	RootCommand.AddCommand(streamsCommand)
	RootCommand.AddCommand(versionCommand)
	RootCommand.AddCommand(watchCommand)
	RootCommand.AddCommand(getCommand)
	RootCommand.PersistentFlags().StringVar(&awsConfig.Endpoint, "endpoint-url", "", "override default endpoint URL")
	RootCommand.PersistentFlags().StringVar(&awsConfig.Region, "region", "", "override profile AWS region")
	RootCommand.PersistentFlags().StringVar(&awsConfig.Profile, "profile", "", "override default AWS profile")
}
