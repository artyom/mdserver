// Program mdurlcheck checks whether given markdown files have any broken
// relative links to other files.
//
// It takes one or more .md files as its arguments, then finds relative links
// (including image links) to other files in them and checks whether such files
// exist on the filesystem.
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
// 	find . -name \*.md -print0 | xargs -0 -P4 mdurlcheck
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
	if len(os.Args) < 2 {
		log.Fatalf("usage: %s file.md ...", filepath.Base(os.Args[0]))
	}
	var exitCode int
	for _, name := range os.Args[1:] {
		if err := run(name); err != nil {
			if err == errDirtyRun {
				exitCode = 1
				continue
			}
			log.Fatal(err)
		}
	}
	os.Exit(exitCode)
}

func run(name string) error {
	b, err := ioutil.ReadFile(name)
	if err != nil {
		return err
	}
	const extensions = parser.CommonExtensions | parser.AutoHeadingIDs ^ parser.MathJax
	doc := parser.NewWithExtensions(extensions).Parse(b)

	idRefs := make(map[string]struct{})
	_ = ast.Walk(doc, ast.NodeVisitorFunc(func(node ast.Node, entering bool) ast.WalkStatus {
		if !entering {
			return ast.GoToNext
		}
		if n, ok := node.(*ast.Heading); ok && n.HeadingID != "" {
			idRefs[n.HeadingID] = struct{}{}
		}
		return ast.GoToNext
	}))

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
		if u.Scheme == "" && u.Host == "" && u.Path == "" && u.Fragment != "" {
			if _, ok := idRefs[u.Fragment]; !ok {
				hadErrors = true
				log.Printf("%s: %q: broken link", name, dst)
			}
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
		return errDirtyRun
	}
	return nil
}

var errDirtyRun = errors.New("some links are not ok")

func fileExists(name string) bool {
	fi, err := os.Stat(name)
	if err != nil {
		return false
	}
	return fi.Mode().IsRegular()
}

func init() { log.SetFlags(0) }
