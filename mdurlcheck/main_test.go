package main

import (
	"bytes"
	"log"
	"strings"
	"testing"
)

func TestMain(t *testing.T) {
	buf := new(bytes.Buffer)
	log.SetOutput(buf)
	intrefs := make(refMap)
	err := run("../testdata", intrefs)
	if err != errDirtyRun {
		t.Fatalf("wrong error value: %+v", err)
	}
	want := strings.TrimSpace(`
../testdata/broken.md: "#duplicate-subheading-1": unstable slug reference, may become incorrect on unrelated header changes
../testdata/broken.md: "../testdata": broken link
../testdata/broken.md: "non-existent.md": broken link
../testdata/broken.md: "#bam": broken link
../testdata/broken.md: "broken.md#boom": broken link (fragment points to non-existent id)
`)
	if got := strings.TrimSpace(buf.String()); got != want {
		t.Logf("expected output:\n%s", want)
		t.Logf("actual output:\n%s", got)
		t.Fatal("output mismatch")
	}
}
