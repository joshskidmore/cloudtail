package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/joshskidmore/cloudtail/cloudwatch"
	"github.com/joshskidmore/cloudtail/config"
	"github.com/joshskidmore/cloudtail/tui"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
)

var watchHeader string

var watchSince string

var watchConfig config.Configuration

var watchOutputConfig config.OutputConfiguration

var watchCommand = &cobra.Command{
	Use:   "watch [log group...]",
	Short: "Continuously stream log events",
	Long:  "Continuously stream log events from one or more log groups. With no log group, pick from a list.",
	Args: func(cmd *cobra.Command, args []string) error {
		if !isatty.IsTerminal(os.Stdout.Fd()) && len(args) != 1 {
			return errors.New("watching without a terminal requires exactly one log group argument")
		}
		return nil
	},
	Run: func(cmd *cobra.Command, args []string) {
		runWatch(args)
	},
}

// runWatch streams log groups. On a terminal it shows the viewer, which
// opens the group picker when args is empty. Otherwise it prints plain lines
// for the one group in args.
func runWatch(args []string) {
	startAt, err := startTime(watchSince)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	svc, err := cloudwatch.NewService(context.Background(), &awsConfig)
	if err != nil {
		fatal(err)
	}
	src := &cloudwatchSource{base: watchConfig, output: &watchOutputConfig, svc: svc}

	if !isatty.IsTerminal(os.Stdout.Fd()) {
		if len(args) != 1 {
			fmt.Fprintln(os.Stderr, "watching without a terminal requires exactly one log group argument")
			os.Exit(1)
		}
		b := src.client(args[0])
		err = b.Stream(context.Background(), startAt(), func(events []cloudwatch.Event, _ bool) {
			for _, e := range events {
				fmt.Println(e.Text)
			}
		})
		fmt.Fprintln(os.Stderr, "Error", err)
		os.Exit(2)
	}

	if err := tui.Run(src, args, startAt, watchHeader); err != nil {
		fmt.Fprintln(os.Stderr, "Error", err)
		os.Exit(2)
	}
}

// startTime returns a function that gives the stream start time for
// --since: a duration before now (e.g. "5m"), or an absolute time.
func startTime(since string) (func() time.Time, error) {
	if since == "" || since == "0" {
		return time.Now, nil
	}
	if d, err := time.ParseDuration(since); err == nil && d > 0 {
		return func() time.Time { return time.Now().Add(-d) }, nil
	}
	t, err := config.ParseTime(since, time.Now())
	if err != nil {
		return nil, fmt.Errorf("--since %q: %v", since, err)
	}
	return func() time.Time { return t }, nil
}

// cloudwatchSource reads log groups and events for the viewer.
type cloudwatchSource struct {
	base   config.Configuration
	output *config.OutputConfiguration
	svc    *cloudwatchlogs.Client
}

// client returns a Client for one group. With --prefix, it limits the client to
// the matching log streams.
func (s *cloudwatchSource) client(group string) *cloudwatch.Client {
	cfg := s.base
	cfg.Group = group
	return cloudwatch.NewClient(s.svc, &cfg, s.output)
}

func (s *cloudwatchSource) Groups(ctx context.Context) ([]string, error) {
	return cloudwatch.NewClient(s.svc, &config.Configuration{}, s.output).LogGroupNames(ctx)
}

func (s *cloudwatchSource) Stream(ctx context.Context, group string, from time.Time, emit func([]tui.Event, bool)) error {
	return s.client(group).Stream(ctx, from, func(events []cloudwatch.Event, history bool) {
		emit(toTUI(events), history)
	})
}

func (s *cloudwatchSource) Fetch(ctx context.Context, group string, from, to time.Time, limit int) ([]tui.Event, error) {
	events, err := s.client(group).Fetch(ctx, from, to, limit)
	return toTUI(events), err
}

func toTUI(events []cloudwatch.Event) []tui.Event {
	out := make([]tui.Event, len(events))
	for i, e := range events {
		out[i] = tui.Event{ID: e.ID, Time: e.Time, Text: e.Text}
	}
	return out
}

func init() {
	watchCommand.Flags().StringVar(&watchHeader, "header", "", "title bar text (default \"[cloudwatch] <log groups>\")")
	watchCommand.Flags().StringVar(&watchSince, "since", "5m", "show events from this long ago (e.g. 30s, 1h) or this local time (e.g. 2026-11-10 15:04:05); 0 for none")
	watchCommand.Flags().StringVar(&watchConfig.Prefix, "prefix", "", "log stream prefix filter")
	watchCommand.Flags().StringVar(&watchConfig.Filter, "filter", "", "event filter pattern")
	watchCommand.Flags().BoolVar(&watchOutputConfig.Raw, "raw", false, "print raw log event without timestamp or stream prefix")
	watchCommand.Flags().BoolVar(&watchOutputConfig.Expand, "expand", false, "indent JSON log messages")
	watchCommand.Flags().BoolVar(&watchOutputConfig.Invert, "invert", false, "invert colors for light terminal themes")
	watchCommand.Flags().BoolVar(&watchOutputConfig.RawString, "rawString", false, "print JSON strings without escaping")
}
