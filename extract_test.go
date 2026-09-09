//nolint:testpackage // exercises unexported helpers
package selfupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"strings"
	"testing"
)

// tarEntry is one file in a test archive.
type tarEntry struct {
	name string
	body string
	typ  byte // 0 means tar.TypeReg
}

// tarGz builds a gzip'd tar with the given entries in order.
func tarGz(t *testing.T, entries ...tarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		typ := e.typ
		if typ == 0 {
			typ = tar.TypeReg
		}
		hdr := &tar.Header{Name: e.name, Mode: 0o755, Size: int64(len(e.body)), Typeflag: typ}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(e.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

const binaryBody = "#!/bin/sh\necho tool\n"

func TestExtractFile_ExtractsNamedEntry(t *testing.T) {
	archive := tarGz(t, tarEntry{name: "README.md", body: "docs"}, tarEntry{name: "tool", body: binaryBody})
	var out bytes.Buffer
	if err := extractFile(bytes.NewReader(archive), "tool", &out); err != nil {
		t.Fatal(err)
	}
	if out.String() != binaryBody {
		t.Fatalf("got %q", out.String())
	}
}

func TestExtractFile_MatchesDotSlashPrefix(t *testing.T) {
	archive := tarGz(t, tarEntry{name: "./tool", body: binaryBody})
	var out bytes.Buffer
	if err := extractFile(bytes.NewReader(archive), "tool", &out); err != nil {
		t.Fatal(err)
	}
	if out.String() != binaryBody {
		t.Fatalf("got %q", out.String())
	}
}

func TestExtractFile_NestedEntryDoesNotMatch(t *testing.T) {
	archive := tarGz(t, tarEntry{name: "bin/tool", body: binaryBody})
	err := extractFile(bytes.NewReader(archive), "tool", &bytes.Buffer{})
	if !errors.Is(err, ErrEntryNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestExtractFile_MissingEntry(t *testing.T) {
	archive := tarGz(t, tarEntry{name: "README.md", body: "docs"})
	var out bytes.Buffer
	err := extractFile(bytes.NewReader(archive), "tool", &out)
	if !errors.Is(err, ErrEntryNotFound) || !strings.Contains(err.Error(), "tool") {
		t.Fatalf("got %v", err)
	}
	if out.Len() != 0 {
		t.Fatal("nothing may be written when the entry is missing")
	}
}

func TestExtractFile_RejectsNonRegularEntry(t *testing.T) {
	archive := tarGz(t, tarEntry{name: "tool", typ: tar.TypeDir})
	err := extractFile(bytes.NewReader(archive), "tool", &bytes.Buffer{})
	if err == nil || errors.Is(err, ErrEntryNotFound) {
		t.Fatalf("a directory named like the binary must be its own error, got %v", err)
	}
}

func TestExtractFile_NotGzip(t *testing.T) {
	if err := extractFile(strings.NewReader("plain text"), "tool", &bytes.Buffer{}); err == nil {
		t.Fatal("expected gzip error")
	}
}
