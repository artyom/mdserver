// Program mdurlcheck checks whether given markdown file has any broken relative
// links to other files.
//
// It takes single .md file as an argument, then finds relative links (including
// image links) to other files in it and checks whether such files exist on the
// filesystem.
//
// Provided with the following file:
//
// 	[Document 1](doc1.md), [document 2](doc2.md), and [another
// 	one](dir/doc.md)
//
//	![program illustration](img/screenshot.jpg "Screenshot")
//
// The program will check whether files doc1.md, doc2.md, dir/doc.md, and
// img/screenshot.jpg exist on disk, relative to the location of provided file.
//
// Program reports any errors on stderr and exits with non-zero exit code.
//
// If you need to check large directory with markdown files for broken
// cross-references, use xargs:
//
// 	find . -name \*.md -print0 | xargs -0 -P4 -n1 mdurlcheck
package main

import (
	"errors"
	"io/ioutil"
	"log"
	"net/url"
	"os"
	"path/filepath"

	"github.com/gomarkdown/markdown/ast"
	"github.com/gomarkdown/markdown/parser"
)

func main() {
	if len(os.Args) != 2 {
		log.Fatalf("usage: %s file.md", filepath.Base(os.Args[0]))
	}
	if err := run(os.Args[1]); err != nil {
		if err == dirtyRun {
			os.Exit(1)
		}
		log.Fatal(err)
	}
}

func run(name string) error {
	b, err := ioutil.ReadFile(name)
	if err != nil {
		return err
	}
	doc := parser.New().Parse(b)
	var hadErrors bool
	walkFn := func(node ast.Node, entering bool) ast.WalkStatus {
		if !entering {
			return ast.GoToNext
		}
		var dst string
		switch n := node.(type) {
		case *ast.Link:
			dst = string(n.Destination)
		case *ast.Image:
			dst = string(n.Destination)
		default:
			return ast.GoToNext
		}
		if dst == "" {
			log.Printf("%s: empty url", name)
			return ast.GoToNext
		}
		u, err := url.Parse(dst)
		if err != nil {
			log.Printf("%s: %q: %v", name, dst, err)
			hadErrors = true
			return ast.GoToNext
		}
		if u.Scheme != "" || u.Host != "" || u.Path == "" {
			return ast.GoToNext
		}
		if !fileExists(filepath.Join(filepath.Dir(name), filepath.FromSlash(u.Path))) {
			hadErrors = true
			log.Printf("%s: %q: broken link", name, dst)
		}
		return ast.GoToNext
	}
	_ = ast.Walk(doc, ast.NodeVisitorFunc(walkFn))
	if hadErrors {
		return dirtyRun
	}
	return nil
}

var dirtyRun = errors.New("some links are not ok")

func fileExists(name string) bool {
	fi, err := os.Stat(name)
	if err != nil {
		return false
	}
	return fi.Mode().IsRegular()
}

func init() { log.SetFlags(0) }
