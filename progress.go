package selfupdate

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// progressBarWidth is the number of cells in the terminal progress bar.
const progressBarWidth = 30

// percentScale converts a fraction to a percentage.
const percentScale = 100

// bytesPerMB converts byte counts for display.
const bytesPerMB = 1024 * 1024

// progressWriter counts bytes written and, when its output is a terminal
// and the total is known, rewrites one line with a bar and percentage.
// Otherwise it prints nothing, so callers print the single
// "Downloading..." line themselves.
type progressWriter struct {
	out   io.Writer
	total int64 // <= 0 when Content-Length is unknown
	n     int64
	tty   bool
	last  int // last percentage drawn; -1 before the first draw
}

// newProgressWriter returns a writer that draws to out only when out is a
// terminal and total is known.
func newProgressWriter(out io.Writer, total int64) *progressWriter {
	return &progressWriter{out: out, total: total, tty: isTerminal(out), last: -1}
}

// Write counts b and redraws the bar when the percentage changed.
func (p *progressWriter) Write(b []byte) (int, error) {
	p.n += int64(len(b))
	p.draw()
	return len(b), nil
}

// Finish ends the line on a terminal. It is a no-op otherwise.
func (p *progressWriter) Finish() {
	if !p.drawing() {
		return
	}
	p.draw()
	_, _ = fmt.Fprintln(p.out)
}

// drawing reports whether there is a terminal and a known total to draw for.
func (p *progressWriter) drawing() bool {
	return p.tty && p.total > 0
}

func (p *progressWriter) draw() {
	if !p.drawing() {
		return
	}
	pct := min(int(p.n*percentScale/p.total), percentScale)
	if pct == p.last {
		return
	}
	p.last = pct
	filled := pct * progressBarWidth / percentScale
	bar := strings.Repeat("=", filled)
	if filled > 0 && filled < progressBarWidth {
		bar = bar[:filled-1] + ">"
	}
	bar += strings.Repeat(" ", progressBarWidth-filled)
	_, _ = fmt.Fprintf(p.out, "\r[%s] %3d%%  %s/%s", bar, pct, formatMB(p.n), formatMB(p.total))
}

// formatMB renders n as megabytes with one decimal.
func formatMB(n int64) string {
	return fmt.Sprintf("%.1f MB", float64(n)/bytesPerMB)
}

// isTerminal reports whether w is an *os.File attached to a character
// device. Buffers, pipes and regular files are not terminals.
func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok || f == nil {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
