// Command mdserver is an http server serving single directory with markdown
// (.md) files. If can render automatically built index of those files and
// render them as html pages.
//
// Its main use-case is reading through directory with documentation written in
// markdown format, i.e. local copy of Github wiki.
//
// To access automatically generated index, request "/?index" path, as
// http://localhost:8080/?index.
package main

import (
	"bytes"
	"fmt"
	"html/template"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/artyom/autoflags"
	"github.com/gomarkdown/markdown"
	"github.com/gomarkdown/markdown/ast"
	"github.com/gomarkdown/markdown/html"
	"github.com/gomarkdown/markdown/parser"
	"github.com/microcosm-cc/bluemonday"
	"golang.org/x/crypto/ssh/terminal"
)

func main() {
	args := runArgs{Dir: ".", Addr: "localhost:8080"}
	autoflags.Parse(&args)
	if err := run(args); err != nil {
		os.Stderr.WriteString(err.Error() + "\n")
		os.Exit(1)
	}
}

type runArgs struct {
	Dir  string `flag:"dir,directory with markdown (.md) files"`
	Addr string `flag:"addr,address to listen"`
	Ghub bool   `flag:"github,rewrite github wiki links to local when rendering"`
	CSS  string `flag:"css,path to custom CSS file"`
}

func run(args runArgs) error {
	h := &mdHandler{
		dir:        args.Dir,
		fileServer: http.FileServer(http.Dir(args.Dir)),
		githubWiki: args.Ghub,
		style:      template.CSS(style),
	}
	if args.CSS != "" {
		b, err := ioutil.ReadFile(args.CSS)
		if err != nil {
			return err
		}
		h.style = template.CSS(b)
	}
	srv := http.Server{
		Addr:         args.Addr,
		Handler:      h,
		ReadTimeout:  time.Second,
		WriteTimeout: 10 * time.Second,
	}
	if runtime.GOOS == "darwin" && terminal.IsTerminal(1) {
		go func() {
			time.Sleep(100 * time.Millisecond)
			exec.Command("open", "http://"+args.Addr+"/?index").Run()
		}()
	}
	return srv.ListenAndServe()
}

type mdHandler struct {
	dir        string
	fileServer http.Handler // initialized as http.FileServer(http.Dir(dir))
	githubWiki bool
	style      template.CSS
}

func (h *mdHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/" && r.URL.RawQuery == "index" {
		indexTemplate.Execute(w, struct {
			Style template.CSS
			Index []indexRecord
		}{Style: h.style, Index: dirIndex(h.dir)})
		return
	}
	if !strings.HasSuffix(r.URL.Path, ".md") {
		h.fileServer.ServeHTTP(w, r)
		return
	}
	// only markdown files are handled below
	p := path.Clean(r.URL.Path)
	if containsDotDot(p) {
		http.Error(w, "invalid URL path", http.StatusBadRequest)
		return
	}
	name := filepath.Join(h.dir, filepath.FromSlash(p))
	b, err := ioutil.ReadFile(name)
	if err != nil {
		if os.IsNotExist(err) {
			http.NotFound(w, r)
			return
		}
		log.Printf("read %q: %v", name, err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	opts := rendererOpts
	if h.githubWiki {
		opts.RenderNodeHook = rewriteGithubWikiLinks
	}
	body := markdown.ToHTML(b, parser.NewWithExtensions(extensions), html.NewRenderer(opts))
	body = policy.SanitizeBytes(body)
	pageTemplate.Execute(w, struct {
		Title string
		Style template.CSS
		Body  template.HTML
	}{
		Title: nameToTitle(filepath.Base(name)),
		Style: h.style,
		Body:  template.HTML(body),
	})
}

func dirIndex(dir string) []indexRecord {
	matches, err := filepath.Glob(filepath.Join(dir, "*.md"))
	if err != nil {
		panic(err)
	}
	index := make([]indexRecord, 0, len(matches))
	for _, s := range matches {
		file := filepath.Base(s)
		title := documentTitle(s)
		if title == "" {
			title = nameToTitle(file)
		}
		index = append(index, indexRecord{Title: title, File: file})
	}
	return index
}

type indexRecord struct {
	Title, File string
}

// documentTitle extracts h1 header from markdown document
func documentTitle(file string) string {
	f, err := os.Open(file)
	if err != nil {
		return ""
	}
	defer f.Close()
	b, err := ioutil.ReadAll(io.LimitReader(f, 1<<17))
	if err != nil {
		return ""
	}
	doc := parser.New().Parse(b)
	var title string
	walkFn := func(node ast.Node, entering bool) ast.WalkStatus {
		if !entering {
			return ast.GoToNext
		}
		switch n := node.(type) {
		case *ast.Heading:
			if n.Level != 1 {
				return ast.GoToNext
			}
			title = string(childLiterals(n))
			return ast.Terminate
		case *ast.Code, *ast.CodeBlock, *ast.BlockQuote:
			return ast.SkipChildren
		}
		return ast.GoToNext
	}
	_ = ast.Walk(doc, ast.NodeVisitorFunc(walkFn))
	return title
}

func childLiterals(node ast.Node) []byte {
	if l := node.AsLeaf(); l != nil {
		return l.Literal
	}
	var out [][]byte
	for _, n := range node.GetChildren() {
		if lit := childLiterals(n); lit != nil {
			out = append(out, lit)
		}
	}
	if out == nil {
		return nil
	}
	return bytes.Join(out, nil)
}

// rewriteGithubWikiLinks is a html.RenderNodeFunc which renders links
// with github wiki destinations as local ones.
//
// Link with "https://github.com/user/project/wiki/Page" destination would be
// rendered as a link to "Page.md"
func rewriteGithubWikiLinks(w io.Writer, node ast.Node, entering bool) (ast.WalkStatus, bool) {
	link, ok := node.(*ast.Link)
	if !ok || !entering {
		return ast.GoToNext, false
	}
	if u, err := url.Parse(string(link.Destination)); err == nil &&
		u.Host == "github.com" && strings.HasSuffix(path.Dir(u.Path), "/wiki") {
		dst := path.Base(u.Path) + ".md"
		fmt.Fprintf(w, "<a href=\"%s\">", url.QueryEscape(dst))
		return ast.GoToNext, true
	}
	return ast.GoToNext, false
}

func nameToTitle(name string) string {
	const suffix = ".md"
	if strings.ContainsAny(name, " ") {
		return strings.TrimSuffix(name, suffix)
	}
	return repl.Replace(strings.TrimSuffix(name, suffix))
}

var repl = strings.NewReplacer("-", " ")

var indexTemplate = template.Must(template.New("index").Parse(indexTpl))
var pageTemplate = template.Must(template.New("page").Parse(pageTpl))

const indexTpl = `<!doctype html><head><title>Index</title>
<style>{{.Style}}</style></head><body>
<h1>Index</h1><ul>
{{range .Index}}<li><a href="{{.File}}">{{.Title}}</a></li>
{{end}}</ul></body>
`

const pageTpl = `<!doctype html><head><title>{{.Title}}</title>
<style>{{.Style}}</style></head><body><nav><a href="/?index">&#10087; index</a></nav><article>
{{.Body}}
</article></body>
`

const extensions = parser.CommonExtensions | parser.AutoHeadingIDs

var rendererOpts = html.RendererOptions{Flags: html.CommonFlags | html.Safelink}
var policy = bluemonday.UGCPolicy()

func containsDotDot(v string) bool {
	if !strings.Contains(v, "..") {
		return false
	}
	for _, ent := range strings.FieldsFunc(v, func(r rune) bool { return r == '/' || r == '\\' }) {
		if ent == ".." {
			return true
		}
	}
	return false
}

const style = `body {
	font-family: "PT Serif", "Droid Serif", serif;
	font-size: 100%;
	line-height: 170%;
	max-width: 45em;
	margin: auto;
	padding-right: 1em;
	padding-left: 1em;
	color: #333;
	background: white;
	text-rendering: optimizeLegibility;
}

@media only screen and (max-device-width:480px) {
	body {
		font-size:110%;
		text-rendering: auto;
	}
}

a {color: #a08941; text-decoration: none;}
a:hover {color: #c6b754; text-decoration: underline;}

footer {
	margin-top: 3em;
	padding-top: 1em;
	padding-bottom: 1em;
	border-top: 1px solid gray;
}

h1 a, h2 a, h3 a, h4 a, h5 a {
	text-decoration: none;
	color: gray;
}
h1 a:hover, h2 a:hover, h3 a:hover, h4 a:hover, h5 a:hover {
	text-decoration: none;
	color: gray;
}
h1, h2, h3, h4, h5 {
	font-family: Georgia, serif;
	font-weight: bold;
	color: gray;
}

h1 {
	font-size: 150%;
}

h2 {
	font-size: 130%;
}

h3 {
	font-size: 110%;
}

h4, h5 {
	font-size: 100%;
	font-style: italic;
}

pre {
	background-color: rgba(200,200,200,0.2);
	color: #1111111;
	padding: 0.5em;
	overflow: auto;
}
code, pre {
	font-size: 90%;
	font-family: "Consolas", "PT Mono", "Lucida Console", monospace;
}

hr { border:none; text-align:center; color:gray; }
hr:after {
	content:"\2766";
	display:inline-block;
	font-size:1.5em;
}

dt code {
	font-weight: bold;
}
dd p {
	margin-top: 0;
}

nav {
	font-size:90%;
	text-align:right;
	padding:.5em;
	border-bottom: 1px solid gray;
}`

//go:generate sh -c "go doc >README"
