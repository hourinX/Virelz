// SPDX-License-Identifier: MIT
// Copyright (c) 2026 hourinX

package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"

	"github.com/hourinX/Virelz/internal/downloader"
)

const (
	renderInterval = 100 * time.Millisecond // minimum gap between redraws of one task
	barWidth       = 24                     // cells used by the progress bar
)

// taskLine is the live row for one download task.
type taskLine struct {
	label  string           // task name shown before the bar
	stage  downloader.Stage // current stage
	done   int64            // bytes written so far
	total  int64            // expected bytes, zero when unknown
	lastAt time.Time        // time of the last rendered row
	failed bool             // whether the task ended in failure
	closed bool             // whether the row has been erased for good
}

// progressRenderer draws one progress bar per task and erases it when the
// task finishes, so only the final report stays on screen.
type progressRenderer struct {
	out      io.Writer         // terminal or redirected stream
	live     bool              // whether the output can be redrawn in place
	mu       sync.Mutex        // serializes updates from parallel tasks
	lines    map[int]*taskLine // row per task index
	order    []int             // stable draw order
	rendered int               // rows currently drawn on screen
}

func newProgressRenderer(out io.Writer) *progressRenderer {
	return &progressRenderer{out: out, live: isTerminal(out), lines: map[int]*taskLine{}}
}

// isTerminal reports whether out can be redrawn in place.
func isTerminal(out io.Writer) bool {
	f, ok := out.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

// isTerminalReader reports whether in is an interactive terminal.
func isTerminalReader(in io.Reader) bool {
	f, ok := in.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

// Slot returns the updater for one task, serialized through the renderer mutex.
func (r *progressRenderer) Slot(index int, label string) func(downloader.Progress) {
	r.mu.Lock()
	if _, ok := r.lines[index]; !ok {
		r.lines[index] = &taskLine{label: label}
		r.order = append(r.order, index)
	}
	r.mu.Unlock()

	return func(p downloader.Progress) {
		r.mu.Lock()
		defer r.mu.Unlock()

		line, ok := r.lines[index]
		if !ok || line.failed || line.closed {
			return
		}
		line.stage = p.Stage
		line.done = p.Done
		line.total = p.Total
		if !line.lastAt.IsZero() && time.Since(line.lastAt) < renderInterval {
			return
		}
		r.redraw()
	}
}

// Done erases the bar of a finished task, leaving no row behind.
func (r *progressRenderer) Done(index int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if line, ok := r.lines[index]; ok {
		line.closed = true
	}
	r.redraw()
}

// Fail stops the bar of a task that ended in an error and erases its row.
func (r *progressRenderer) Fail(index int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if line, ok := r.lines[index]; ok {
		line.failed = true
		line.closed = true
	}
	r.redraw()
}

// Close removes any bar still on screen so later output starts cleanly.
func (r *progressRenderer) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, line := range r.lines {
		line.closed = true
	}
	r.redraw()
}

// redraw erases every row drawn so far and repaints only the live ones. A
// finished task leaves nothing behind, so its row disappears instead of
// turning into a blank line.
func (r *progressRenderer) redraw() {
	if !r.live {
		return
	}

	var b strings.Builder
	b.WriteString("\r")
	if r.rendered > 0 {
		b.WriteString("\x1b[2K") // clear the row the cursor sits on
		for i := 1; i < r.rendered; i++ {
			b.WriteString("\x1b[1A\x1b[2K") // clear the rows above it
		}
	}

	active := 0
	for _, index := range r.order {
		line := r.lines[index]
		if line.closed {
			continue
		}
		b.WriteString(line.render())
		b.WriteString("\n")
		line.lastAt = time.Now()
		active++
	}
	if active > 0 {
		b.WriteString("\x1b[1A") // the last newline was a row break, not a new line
	}

	r.rendered = active
	_, _ = io.WriteString(r.out, b.String())
}

func (l *taskLine) render() string {
	if l.failed {
		return fmt.Sprintf("%s failed", l.label)
	}
	if l.stage == downloader.StageMerging {
		return fmt.Sprintf("%s  %s merging", l.label, bar(1))
	}
	if l.total <= 0 {
		return fmt.Sprintf("%s  %s %s", l.label, bar(0), formatBytes(l.done))
	}
	ratio := float64(l.done) / float64(l.total)
	if ratio > 1 {
		ratio = 1
	}
	return fmt.Sprintf("%s  %s %3.0f%%  %s / %s", l.label, bar(ratio), ratio*100, formatBytes(l.done), formatBytes(l.total))
}

// bar renders a fixed-width progress bar for a ratio between zero and one.
func bar(ratio float64) string {
	if ratio < 0 {
		ratio = 0
	}
	filled := int(ratio*barWidth + 0.5)
	if filled > barWidth {
		filled = barWidth
	}
	return "[" + strings.Repeat("=", filled) + strings.Repeat(" ", barWidth-filled) + "]"
}
