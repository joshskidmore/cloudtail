package tui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Event is one formatted log event.
type Event struct {
	ID   string // unique event ID, used to drop duplicates
	Time int64  // milliseconds since the Unix epoch
	Text string
}

// Source reads log groups and log events. The viewer calls it from
// background goroutines.
type Source interface {
	// Groups returns the names of all log groups.
	Groups(ctx context.Context) ([]string, error)
	// Stream sends events of group from the given time until ctx is
	// cancelled. history is true for events from before the call.
	Stream(ctx context.Context, group string, from time.Time, emit func(events []Event, history bool)) error
	// Fetch returns up to limit events of group in the time range
	// [from, to], oldest first.
	Fetch(ctx context.Context, group string, from, to time.Time, limit int) ([]Event, error)
}

// windowStep is how much history one fetch covers: the "[" and "]" keys,
// and the window that "t" loads around a time.
const windowStep = 15 * time.Minute

// fetchLimit caps the events that one fetch loads per group. The loaded
// range ends at the last event when a fetch reaches the cap.
const fetchLimit = 10000

type groupsMsg struct {
	names []string
	err   error
}

type eventsMsg struct {
	gen     int
	group   int
	events  []Event
	history bool
}

type fetchMsg struct {
	gen    int
	group  int
	events []Event
	until  time.Time // the fetch covers [from, until)
	err    error
}

type streamErrMsg struct {
	gen   int
	group string
	err   error
}

// runner starts and stops the background streams. The model holds a pointer
// to it, so all copies of the model share it.
type runner struct {
	p      *tea.Program
	src    Source
	cancel context.CancelFunc
	gen    int // increases on each restart; messages from older streams are dropped
	ctx    context.Context
}

// start stops any running streams and streams the given groups from the
// given time.
func (r *runner) start(groups []string, from time.Time) {
	r.stop()
	r.gen++
	gen := r.gen
	r.ctx, r.cancel = context.WithCancel(context.Background())
	ctx := r.ctx
	for gi, g := range groups {
		go func(gi int, g string) {
			err := r.src.Stream(ctx, g, from, func(events []Event, history bool) {
				r.p.Send(eventsMsg{gen, gi, events, history})
			})
			if err != nil && ctx.Err() == nil {
				r.p.Send(streamErrMsg{gen, g, err})
			}
		}(gi, g)
	}
}

func (r *runner) stop() {
	if r.cancel != nil {
		r.cancel()
	}
}

// fetch loads the time range [from, to) for each group. When a group hits
// fetchLimit, its range ends at its last event.
func (r *runner) fetch(groups []string, from, to time.Time) {
	gen, ctx := r.gen, r.ctx
	for gi, g := range groups {
		go func(gi int, g string) {
			events, err := r.src.Fetch(ctx, g, from, to.Add(-time.Millisecond), fetchLimit)
			until := to
			if len(events) >= fetchLimit {
				until = time.UnixMilli(events[len(events)-1].Time)
			}
			if ctx.Err() == nil {
				r.p.Send(fetchMsg{gen, gi, events, until, err})
			}
		}(gi, g)
	}
}

// loadGroups returns a command that lists the log groups.
func (r *runner) loadGroups() tea.Cmd {
	return func() tea.Msg {
		names, err := r.src.Groups(context.Background())
		return groupsMsg{names, err}
	}
}
