// Command mdrename renames/moves single markdown (.md) file, while scanning for
// all .md files under current working directory, looking for links referencing
// moved file. If it finds such files, it then updates links so they point to new
// location.
//
// Usage:
//
// 	mdrename file.md new-name.md
//
// Note that since it may potentially update multiple files, the whole operation is
// not atomic, it is advisable to only run it over files versioned by VCS which can
// be restored in case of any errors.
//
// Currently only inline links like [link](dst.md) are supported; links like
// [link][id] are NOT supported. The reason for this is that links are updated by
// substring replacements inside text, this may lead to some invalid replacements,
// and handling only inline links somewhat reduces risk of invalid replacements.
// Please check results before committing them.
//
// If program succeeds in renaming file and updating all found references, and
// "mdurlcheck" tool exists in PATH, then "mdurlcheck ." is called as a final step,
// this way you can see whether any links (especially non-inline ones) were missed.
package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/gomarkdown/markdown/ast"
	"github.com/gomarkdown/markdown/parser"
)

func main() {
	if len(os.Args) != 3 {
		os.Stderr.WriteString(usage)
		os.Exit(2)
	}
	if err := run(os.Args[1], os.Args[2]); err != nil {
		os.Stderr.WriteString(err.Error() + "\n")
		os.Exit(1)
	}
}

func run(src, dst string) error {
	if src == "" || dst == "" {
		return fmt.Errorf("both source and destination must be set")
	}
	if !strings.HasSuffix(src, ".md") || !strings.HasSuffix(dst, ".md") {
		return fmt.Errorf("both source and destination must have '.md' suffix")
	}
	if src == dst {
		return nil
	}
	if fileExists(dst) {
		return fmt.Errorf("destination %q already exists", dst)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0777); err != nil {
		return err
	}
	if err := os.Rename(src, dst); err != nil {
		return err
	}
	if err := updateRenamedFile(src, dst); err != nil {
		return err
	}
	var errCnt int
	err := filepath.Walk(".", func(name string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if base := filepath.Base(name); fi.IsDir() && base != "." && strings.HasPrefix(base, ".") {
			return filepath.SkipDir
		}
		if fi.IsDir() || !strings.HasSuffix(name, ".md") {
			return nil
		}
		if err := processFile(name, src, dst); err != nil {
			errCnt++
			log.Printf("%q: %v", name, err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if errCnt > 0 {
		return fmt.Errorf("had errors processing %d file(s)", errCnt)
	}
	if _, err := exec.LookPath("mdurlcheck"); err == nil {
		cmd := exec.Command("mdurlcheck", ".")
		cmd.Stderr = os.Stderr
		cmd.Stdout = os.Stdout
		return cmd.Run()
	}
	return nil
}

// updateRenamedFile updates relative links in already renamed file. Its
// original name was src, its new name is dst.
func updateRenamedFile(src, dst string) error {
	b, err := ioutil.ReadFile(dst)
	if err != nil {
		return err
	}
	var repl []string
	doc := parser.NewWithExtensions(extensions).Parse(b)
	var walkErr error
	walkFn := func(node ast.Node, entering bool) ast.WalkStatus {
		if !entering {
			return ast.GoToNext
		}
		var link string
		switch n := node.(type) {
		case *ast.Link:
			link = string(n.Destination)
		case *ast.Image:
			link = string(n.Destination)
		default:
			return ast.GoToNext
		}
		u, err := url.Parse(link)
		if err != nil || u.Scheme != "" || u.Host != "" || u.Path == "" {
			return ast.GoToNext
		}
		filename := filepath.Join(filepath.Dir(src), filepath.FromSlash(u.Path))
		relPath, err := filepath.Rel(filepath.Dir(dst), filename)
		if err != nil {
			walkErr = err
			return ast.Terminate
		}
		u2 := &url.URL{Path: filepath.ToSlash(relPath), Fragment: u.Fragment}
		log.Printf("%s: %q -> %q", dst, link, u2)
		repl = append(repl, "("+link+")", "("+u2.String()+")")
		return ast.GoToNext
	}
	_ = ast.Walk(doc, ast.NodeVisitorFunc(walkFn))
	if walkErr != nil {
		return err
	}
	if len(repl) == 0 {
		return nil
	}
	r := strings.NewReplacer(repl...)
	return ioutil.WriteFile(dst, []byte(r.Replace(string(b))), 0666)
}

func processFile(name, src, dst string) error {
	b, err := ioutil.ReadFile(name)
	if err != nil {
		return err
	}
	// cheap check first
	if !bytes.Contains(b, []byte(filepath.Base(src))) {
		return nil
	}
	var repl []string
	doc := parser.NewWithExtensions(extensions).Parse(b)
	var walkErr error
	walkFn := func(node ast.Node, entering bool) ast.WalkStatus {
		if !entering {
			return ast.GoToNext
		}
		var link string
		switch n := node.(type) {
		case *ast.Link:
			link = string(n.Destination)
		default:
			return ast.GoToNext
		}
		u, err := url.Parse(link)
		if err != nil || u.Scheme != "" || u.Host != "" || u.Path == "" {
			return ast.GoToNext
		}
		filename := filepath.Join(filepath.Dir(name), filepath.FromSlash(u.Path))
		if filename != src {
			return ast.GoToNext
		}
		relPath, err := filepath.Rel(filepath.Dir(name), dst)
		if err != nil {
			walkErr = err
			return ast.Terminate
		}
		u2 := &url.URL{Path: filepath.ToSlash(relPath), Fragment: u.Fragment}
		log.Printf("%s: %q -> %q", name, link, u2)
		repl = append(repl, "("+link+")", "("+u2.String()+")")
		return ast.GoToNext
	}
	_ = ast.Walk(doc, ast.NodeVisitorFunc(walkFn))
	if walkErr != nil {
		return err
	}
	if len(repl) == 0 {
		return nil
	}
	// FIXME: probably using regexp.Regexp.ReplaceAllLiteral may be a better
	// idea as it would be possible to exactly handle word boundaries this
	// way
	r := strings.NewReplacer(repl...)
	return ioutil.WriteFile(name, []byte(r.Replace(string(b))), 0666)
}

func fileExists(name string) bool {
	fi, err := os.Stat(name)
	if err != nil {
		return false
	}
	return fi.Mode().IsRegular()
}

func init() { log.SetFlags(0) }

const extensions = parser.CommonExtensions | parser.AutoHeadingIDs ^ parser.MathJax

const usage = `usage: mdrename file.md new-name.md

Command mdrename renames/moves single markdown (.md) file, while scanning for
all .md files under current working directory, looking for links referencing
moved file. If it finds such files, it then updates links so they point to new
location.

Note that since it may potentially update multiple files, the whole operation is
not atomic, it is advisable to only run it over files versioned by VCS which can
be restored in case of any errors.

Currently only inline links like [link](dst.md) are supported; links like
[link][id] are NOT supported. The reason for this is that links are updated by
substring replacements inside text, this may lead to some invalid replacements,
and handling only inline links somewhat reduces risk of invalid replacements.
Please check results before committing them.

If program succeeds in renaming file and updating all found references, and
"mdurlcheck" tool exists in PATH, then "mdurlcheck ." is called as a final step,
this way you can see whether any links (especially non-inline ones) were missed.
`

//go:generate sh -c "go doc >README"
