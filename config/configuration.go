package config

import (
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
)

type Configuration struct {
	Group      string
	Prefix     string
	Start      string
	End        string
	Filter     string
	Descending bool
	OrderBy    string
}

// Define the order of time formats to attempt to use to parse our input absolute time
var absoluteTimeFormats = []string{
	time.RFC3339,

	"2006-01-02",          // Simple date
	"2006-01-02 15:04:05", // Simple date & time
}

// ParseTime parses a relative duration (e.g. "-5m") or an absolute time.
// Absolute times without a zone are in the zone of currentTime.
// Provide the currentTime as a parameter to support relative time.
func ParseTime(timeStr string, currentTime time.Time) (time.Time, error) {
	relative, err := time.ParseDuration(timeStr)
	if err == nil {
		return currentTime.Add(relative), nil
	}

	// Iterate over available absolute time formats until we find one that works
	for _, timeFormat := range absoluteTimeFormats {
		absolute, err := time.ParseInLocation(timeFormat, timeStr, currentTime.Location())

		if err == nil {
			return absolute, err
		}
	}

	return time.Time{}, errors.New("Could not parse relative or absolute time")
}

func (c *Configuration) DescribeLogGroupsInput() *cloudwatchlogs.DescribeLogGroupsInput {
	input := cloudwatchlogs.DescribeLogGroupsInput{}
	if c.Prefix != "" {
		input.LogGroupNamePrefix = aws.String(c.Prefix)
	}
	return &input
}

func (c *Configuration) DescribeLogStreamsInput() *cloudwatchlogs.DescribeLogStreamsInput {
	input := cloudwatchlogs.DescribeLogStreamsInput{}
	input.LogGroupName = aws.String(c.Group)
	input.Descending = aws.Bool(c.Descending)

	if c.OrderBy != "" {
		input.OrderBy = types.OrderBy(c.OrderBy)
	}

	if c.Prefix != "" {
		input.LogStreamNamePrefix = aws.String(c.Prefix)
	}

	return &input
}

func (c *Configuration) FilterLogEventsInput() (*cloudwatchlogs.FilterLogEventsInput, error) {
	input := cloudwatchlogs.FilterLogEventsInput{}
	input.LogGroupName = aws.String(c.Group)

	if c.Prefix != "" {
		input.LogStreamNamePrefix = aws.String(c.Prefix)
	}

	currentTime := time.Now()
	absoluteStartTime := currentTime
	if c.Start != "" {
		st, err := ParseTime(c.Start, currentTime)
		if err != nil {
			return nil, fmt.Errorf("--start %q: %v", c.Start, err)
		}
		absoluteStartTime = st
	}
	input.StartTime = aws.Int64(absoluteStartTime.UnixMilli())

	if c.End != "" {
		et, err := ParseTime(c.End, currentTime)
		if err != nil {
			return nil, fmt.Errorf("--stop %q: %v", c.End, err)
		}
		input.EndTime = aws.Int64(et.UnixMilli())
	}

	if c.Filter != "" {
		input.FilterPattern = aws.String(c.Filter)
	}

	return &input, nil
}
