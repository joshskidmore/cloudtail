package tui

import (
	"fmt"
	"regexp"
)

// AWS Lambda writes START, END, and REPORT lines around each invocation, and
// its runtimes put the request ID in most log lines. These helpers find the
// request ID of each entry so the viewer can group and summarize requests.

const uuid = `[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`

var (
	// prefixPattern splits the "[time] (stream) message" line format.
	prefixPattern = regexp.MustCompile(`(?s)^\[([^\]]*)\] \(([^)]*)\) (.*)$`)

	startPattern  = regexp.MustCompile(`\bSTART RequestId: (` + uuid + `)`)
	reportPattern = regexp.MustCompile(`\bREPORT RequestId: (` + uuid + `)\s+Duration: ([\d.]+) ms\s+Billed Duration: (\d+) ms\s+Memory Size: (\d+) MB\s+Max Memory Used: (\d+) MB(?:\s+Init Duration: ([\d.]+) ms)?`)

	// requestPatterns find a request ID in other lines: Lambda's own lines,
	// the tab-separated Node.js and Python formats, and JSON log fields.
	requestPatterns = []*regexp.Regexp{
		regexp.MustCompile(`\bRequestId: (` + uuid + `)`),
		regexp.MustCompile(`\t(` + uuid + `)\t`),
		regexp.MustCompile(`"(?i:aws_?request_?id|request_?id)"\s*:\s*"(` + uuid + `)"`),
	}
)

// report holds the numbers from a Lambda REPORT line.
type report struct {
	duration, memory, maxMemory, init string
}

func (r report) String() string {
	s := fmt.Sprintf("%s ms · %s/%s MB", r.duration, r.maxMemory, r.memory)
	if r.init != "" {
		s += " · init " + r.init + " ms"
	}
	return s
}

// splitPrefix returns the time, stream, and message of a log line. Raw lines
// have no prefix, so time and stream are empty.
func splitPrefix(plain string) (when, stream, msg string) {
	if p := prefixPattern.FindStringSubmatch(plain); p != nil {
		return p[1], p[2], p[3]
	}
	return "", "", plain
}

// track finds the request ID of a new entry from group gi and records
// REPORT numbers. A Lambda log stream runs one request at a time, so a line
// with no request ID belongs to the request that open holds for its stream.
func (m *model) track(plain string, gi int, open map[string]string) string {
	_, stream, _ := splitPrefix(plain)
	if stream != "" {
		stream = fmt.Sprint(gi) + "|" + stream
	}
	id := ""
	for _, re := range requestPatterns {
		if s := re.FindStringSubmatch(plain); s != nil {
			id = s[1]
			break
		}
	}
	if s := startPattern.FindStringSubmatch(plain); s != nil && stream != "" {
		open[stream] = s[1]
	}
	if id == "" && stream != "" {
		id = open[stream]
	}
	if s := reportPattern.FindStringSubmatch(plain); s != nil {
		m.reports[s[1]] = report{s[2], s[4], s[5], s[6]}
		if stream != "" && open[stream] == s[1] {
			delete(open, stream)
		}
	}
	return id
}
