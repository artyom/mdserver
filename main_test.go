package main

import (
	"bytes"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLazyRendering(t *testing.T) {
	srv := httptest.NewServer(&mdHandler{dir: "testdata"})
	defer srv.Close()
	logBuf := new(bytes.Buffer)
	log.SetOutput(logBuf)
	r, err := http.Get(srv.URL + "/hello.md")
	if err != nil {
		t.Fatal(err)
	}
	if r.StatusCode != http.StatusOK {
		t.Fatalf("invalid status on first call, want 200, got: %q", r.Status)
	}
	if _, err := io.Copy(ioutil.Discard, r.Body); err != nil {
		t.Fatal(err)
	}
	lastmod := r.Header.Get("Last-Modified")
	if lastmod == "" {
		t.Fatalf("no or empty Last-Modified header; response headers are:\n%v", r.Header)
	}
	r.Body.Close()
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/hello.md", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("If-Modified-Since", lastmod)
	r, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if r.StatusCode != http.StatusNotModified {
		t.Fatalf("unexpected status on second call, want 304, got: %q", r.Status)
	}
	defer r.Body.Close()
	if cnt := strings.Count(logBuf.String(), "lazyReadSeeker init()"); cnt != 1 {
		t.Fatalf("want 1 logged lazyReadSeeker init call, got %d; full log:\n%s", cnt, logBuf.String())
	}
}

func init() { testRun = true }
