// Package cloudwatch reads log groups, streams, and events from AWS
// CloudWatch Logs.
package cloudwatch

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/TylerBrock/colorjson"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
	"github.com/fatih/color"
	cfg "github.com/joshskidmore/cloudtail/config"
)

// maxFetchFailures is the number of consecutive failed fetches after which
// Stream gives up.
const maxFetchFailures = 5

// Client reads CloudWatch Logs for one configuration.
type Client struct {
	config *cfg.Configuration
	output *cfg.OutputConfiguration
	cwl    *cloudwatchlogs.Client
}

// NewService loads the AWS configuration and creates a CloudWatch Logs
// service client. Create it once and share it; it may prompt for an MFA token.
func NewService(ctx context.Context, awsConfig *cfg.AWSConfiguration) (*cloudwatchlogs.Client, error) {
	opts := []func(*config.LoadOptions) error{
		config.WithAssumeRoleCredentialOptions(func(o *stscreds.AssumeRoleOptions) {
			o.TokenProvider = stscreds.StdinTokenProvider
		}),
	}
	if awsConfig.Region != "" {
		opts = append(opts, config.WithRegion(awsConfig.Region))
	}
	if awsConfig.Profile != "" {
		opts = append(opts, config.WithSharedConfigProfile(awsConfig.Profile))
	}
	awsCfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, err
	}
	return cloudwatchlogs.NewFromConfig(awsCfg, func(o *cloudwatchlogs.Options) {
		if awsConfig.Endpoint != "" {
			o.BaseEndpoint = aws.String(awsConfig.Endpoint)
		}
	}), nil
}

// NewClient creates a Client that reads through the service client for the
// provided config.
func NewClient(
	cwl *cloudwatchlogs.Client,
	config *cfg.Configuration,
	outputConfig *cfg.OutputConfiguration,
) *Client {
	return &Client{cwl: cwl, config: config, output: outputConfig}
}

// GetLogGroups gets the log groups from AWS given the client configuration
func (b *Client) GetLogGroups(ctx context.Context) ([]types.LogGroup, error) {
	var groups []types.LogGroup
	p := cloudwatchlogs.NewDescribeLogGroupsPaginator(b.cwl, b.config.DescribeLogGroupsInput())
	for p.HasMorePages() {
		out, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		groups = append(groups, out.LogGroups...)
	}
	return groups, nil
}

// GetLogStreams gets the log streams from AWS given the client configuration
func (b *Client) GetLogStreams(ctx context.Context) ([]types.LogStream, error) {
	var streams []types.LogStream
	p := cloudwatchlogs.NewDescribeLogStreamsPaginator(b.cwl, b.config.DescribeLogStreamsInput())
	for p.HasMorePages() {
		out, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		streams = append(streams, out.LogStreams...)
	}
	return streams, nil
}

// GetEvents prints events from AWS given the client configuration
func (b *Client) GetEvents(ctx context.Context) error {
	formatter := b.output.Formatter()
	input, err := b.config.FilterLogEventsInput()
	if err != nil {
		return err
	}
	p := cloudwatchlogs.NewFilterLogEventsPaginator(b.cwl, input)
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return err
		}
		for _, event := range page.Events {
			if b.output.Pretty {
				fmt.Println(formatEvent(formatter, event))
			} else {
				fmt.Println(*event.Message)
			}
		}
	}
	return nil
}

// Event is one formatted log event.
type Event struct {
	ID   string // CloudWatch event ID
	Time int64  // milliseconds since the Unix epoch
	Text string
}

// Stream continuously fetches log events from the given time and passes
// each page of events to emit. history is true for events from the first
// fetch, which covers the time before the stream started. A failed fetch is
// retried with exponential backoff (1s to 16s). Stream returns the error
// after maxFetchFailures consecutive failures, or when ctx is cancelled.
func (b *Client) Stream(ctx context.Context, from time.Time, emit func(events []Event, history bool)) error {
	var lastSeenTime *int64
	var seenEventIDs map[string]bool
	formatter := b.output.Formatter()
	input, err := b.config.FilterLogEventsInput()
	if err != nil {
		return err
	}
	input.StartTime = aws.Int64(from.UnixMilli())
	input.EndTime = nil
	history := true
	failures := 0

	updateLastSeenTime := func(ts *int64) {
		if lastSeenTime == nil || *ts > *lastSeenTime {
			lastSeenTime = ts
			seenEventIDs = make(map[string]bool)
		}
	}

	for {
		in := *input
		p := cloudwatchlogs.NewFilterLogEventsPaginator(b.cwl, &in)
		var err error
		for p.HasMorePages() {
			var page *cloudwatchlogs.FilterLogEventsOutput
			if page, err = p.NextPage(ctx); err != nil {
				break
			}
			var events []Event
			for _, event := range page.Events {
				updateLastSeenTime(event.Timestamp)
				if !seenEventIDs[*event.EventId] {
					events = append(events, Event{*event.EventId, *event.Timestamp, b.format(formatter, event)})
					seenEventIDs[*event.EventId] = true
				}
			}
			if len(events) > 0 {
				emit(events, history)
			}
		}
		wait := time.Second
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			failures++
			if failures >= maxFetchFailures {
				return err
			}
			wait = time.Second << (failures - 1)
		} else {
			failures = 0
			history = false
		}
		if lastSeenTime != nil {
			input.StartTime = aws.Int64(*lastSeenTime)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
}

// Fetch returns the log events from the time range [from, to], oldest
// first. It stops after limit events.
func (b *Client) Fetch(ctx context.Context, from, to time.Time, limit int) ([]Event, error) {
	formatter := b.output.Formatter()
	input, err := b.config.FilterLogEventsInput()
	if err != nil {
		return nil, err
	}
	input.StartTime = aws.Int64(from.UnixMilli())
	input.EndTime = aws.Int64(to.UnixMilli())
	var events []Event
	p := cloudwatchlogs.NewFilterLogEventsPaginator(b.cwl, input)
	for p.HasMorePages() && len(events) < limit {
		page, err := p.NextPage(ctx)
		if err != nil {
			return events, err
		}
		for _, event := range page.Events {
			if len(events) >= limit {
				break
			}
			events = append(events, Event{*event.EventId, *event.Timestamp, b.format(formatter, event)})
		}
	}
	return events, nil
}

// LogGroupNames returns the names of all log groups.
func (b *Client) LogGroupNames(ctx context.Context) ([]string, error) {
	groups, err := b.GetLogGroups(ctx)
	var names []string
	for _, group := range groups {
		names = append(names, *group.LogGroupName)
	}
	return names, err
}

// format returns the event as raw text or in the "[time] (stream) message"
// form, without a trailing newline.
func (b *Client) format(formatter *colorjson.Formatter, event types.FilteredLogEvent) string {
	message := *event.Message
	if !b.output.Raw {
		message = formatEvent(formatter, event)
	}
	return strings.TrimRight(message, "\n")
}

// formatEvent returns a CloudWatch log event as a formatted string using the provided formatter
func formatEvent(formatter *colorjson.Formatter, event types.FilteredLogEvent) string {
	red := color.New(color.FgRed).SprintFunc()
	white := color.New(color.FgWhite).SprintFunc()

	str := aws.ToString(event.Message)
	bytes := []byte(str)
	date := time.UnixMilli(aws.ToInt64(event.Timestamp))
	dateStr := date.Format(time.RFC3339)
	streamStr := aws.ToString(event.LogStreamName)
	jl := map[string]interface{}{}

	if err := json.Unmarshal(bytes, &jl); err != nil {
		return fmt.Sprintf("[%s] (%s) %s", red(dateStr), white(streamStr), str)
	}

	output, _ := formatter.Marshal(jl)
	return fmt.Sprintf("[%s] (%s) %s", red(dateStr), white(streamStr), output)
}
