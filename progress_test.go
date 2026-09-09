//nolint:testpackage // exercises unexported helpers and the tty flag
package selfupdate

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestProgressWriter_NonTerminalIsSilent(t *testing.T) {
	var out bytes.Buffer
	const total = 100
	p := newProgressWriter(&out, total)
	n, err := p.Write(make([]byte, 40))
	if err != nil || n != 40 {
		t.Fatalf("Write: %d, %v", n, err)
	}
	p.Finish()
	if out.Len() != 0 {
		t.Fatalf("a buffer is not a terminal; got %q", out.String())
	}
	if p.n != 40 {
		t.Fatalf("counted %d bytes, want 40", p.n)
	}
}

func TestProgressWriter_UnknownTotalNeverDraws(t *testing.T) {
	var out bytes.Buffer
	p := newProgressWriter(&out, -1)
	p.tty = true // force the terminal path; the unknown total must still win
	_, _ = p.Write([]byte("data"))
	p.Finish()
	if out.Len() != 0 {
		t.Fatalf("got %q", out.String())
	}
}

func TestProgressWriter_TerminalDrawsBar(t *testing.T) {
	var out bytes.Buffer
	const total = 100
	p := &progressWriter{out: &out, total: total, tty: true, last: -1}
	_, _ = p.Write(make([]byte, 45))
	first := out.String()
	if !strings.HasPrefix(first, "\r[") || !strings.Contains(first, " 45%") || !strings.Contains(first, ">") {
		t.Fatalf("first draw: %q", first)
	}
	out.Reset()
	_, _ = p.Write(nil) // same percentage: no redraw
	if out.Len() != 0 {
		t.Fatalf("redrew without a percentage change: %q", out.String())
	}
	_, _ = p.Write(make([]byte, 55))
	p.Finish()
	end := out.String()
	if !strings.Contains(end, "100%") || !strings.HasSuffix(end, "\n") {
		t.Fatalf("final draw: %q", end)
	}
	if strings.Contains(end, ">") {
		t.Fatalf("a full bar has no head marker: %q", end)
	}
}

func TestProgressWriter_ClampsOver100(t *testing.T) {
	var out bytes.Buffer
	const total = 10
	p := &progressWriter{out: &out, total: total, tty: true, last: -1}
	_, _ = p.Write(make([]byte, 25))
	if !strings.Contains(out.String(), "100%") {
		t.Fatalf("got %q", out.String())
	}
}

func TestFormatMB(t *testing.T) {
	const twelvePointThree = 12*1024*1024 + 300*1024
	if got := formatMB(twelvePointThree); got != "12.3 MB" {
		t.Fatalf("got %q", got)
	}
	if got := formatMB(0); got != "0.0 MB" {
		t.Fatalf("got %q", got)
	}
}

func TestIsTerminal(t *testing.T) {
	if isTerminal(&bytes.Buffer{}) {
		t.Error("a buffer must not be a terminal")
	}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	defer func() { _ = w.Close() }()
	if isTerminal(w) {
		t.Error("the write end of a pipe must not be a terminal")
	}
	var nilFile *os.File
	if isTerminal(nilFile) {
		t.Error("a nil *os.File must not be a terminal")
	}
}
