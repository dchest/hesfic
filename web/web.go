package web

import (
	"bytes"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"path"
	"sort"

	"github.com/dchest/hesfic/block"
	"github.com/dchest/hesfic/dir"
	"github.com/dchest/hesfic/snapshot"
)

var (
	indexTemplate = template.Must(template.New("index").Parse(indexTemplateSrc))
	dirTemplate   = template.Must(template.New("dir").Parse(dirTemplateSrc))
	fileTemplate  = template.Must(template.New("file").Parse(fileTemplateSrc))
)

type snapshotDesc struct {
	Name       string
	Time       string
	SourcePath string
	DirRef     string
	DirRefPart string
	Comment    string
}

func indexHandler(w http.ResponseWriter, req *http.Request) {
	// List snapshots.
	names, err := snapshot.GetNames()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	rows := make([]snapshotDesc, len(names))
	for i, name := range names {
		var r snapshotDesc
		r.Name = name
		si, err := snapshot.LoadInfo(name)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		r.Comment = si.Comment
		r.SourcePath = si.SourcePath
		r.Time = si.Time.Local().Format("02 Jan 2006 15:04:05 Mon")
		r.DirRef = si.DirRef.String()
		r.DirRefPart = r.DirRef[:12] + "..."
		rows[len(rows)-1-i] = r // in reverse
	}

	var b bytes.Buffer
	if err := indexTemplate.Execute(&b,
		&struct {
			Title     string
			Snapshots []snapshotDesc
		}{
			"Snapshots",
			rows,
		}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	b.WriteTo(w)
}

type fileDesc struct {
	IsDir bool
	Name  string
	Mode  string
	Time  string
	Size  string
	Ref   string
}

type fileDescSlice []fileDesc

func (p fileDescSlice) Len() int      { return len(p) }
func (p fileDescSlice) Swap(i, j int) { p[i], p[j] = p[j], p[i] }
func (p fileDescSlice) Less(i, j int) bool {
	switch {
	case p[i].IsDir && !p[j].IsDir:
		return true
	case !p[i].IsDir && p[j].IsDir:
		return false
	default:
		return p[i].Name < p[j].Name
	}
	panic("unreachable")
}

func sizeString(n int64) string {
	//XXX this is copied from main.go.
	const (
		KiB = 1024
		MiB = 1024 * KiB
		GiB = 1024 * MiB
		TiB = 1024 * GiB
	)
	if n >= TiB {
		return fmt.Sprintf("%5.1fT", float64(n)/GiB)
	}
	if n >= GiB {
		return fmt.Sprintf("%5.1fG", float64(n)/GiB)
	}
	if n >= MiB {
		return fmt.Sprintf("%5.1fM", float64(n)/MiB)
	}
	if n >= KiB {
		return fmt.Sprintf("%5.1fK", float64(n)/KiB)
	}
	return fmt.Sprintf("%6d", n)
}

func dirHandler(w http.ResponseWriter, req *http.Request) {
	refName := path.Base(req.URL.Path)
	//TODO reject other paths.
	dirRef := block.RefFromHex([]byte(refName))
	if dirRef == nil {
		http.Error(w, fmt.Sprintf("Bad ref"), http.StatusBadRequest)
		return
	}
	files, err := dir.LoadDirectory(dirRef)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	rows := make([]fileDesc, len(files))
	for i, f := range files {
		var r fileDesc
		r.IsDir = f.Mode.IsDir()
		r.Name = f.Name
		r.Mode = f.Mode.String()
		r.Time = f.ModTime.Local().Format("02 Jan 2006 15:04")
		r.Size = sizeString(f.Size)
		r.Ref = f.Ref.String()
		rows[i] = r
	}
	sort.Sort(fileDescSlice(rows))
	var b bytes.Buffer
	if err := dirTemplate.Execute(&b,
		&struct {
			Title  string
			DirRef *block.Ref
			Files  []fileDesc
		}{
			"Directory",
			dirRef,
			rows,
		}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	b.WriteTo(w)
}

func fileHandler(w http.ResponseWriter, req *http.Request) {
	//XXX Not implemented.
	var b bytes.Buffer
	if err := fileTemplate.Execute(&b,
		&struct {
			Title string
		}{
			"File",
		}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	b.WriteTo(w)
}

func Serve(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	http.HandleFunc("/", indexHandler)
	http.HandleFunc("/dir/", dirHandler)
	http.HandleFunc("/file/", fileHandler)
	fmt.Printf("Listening %s...\n", ln.Addr())
	return http.Serve(ln, nil)
}

//XXX External Bootstrap is only for testing.
const commonHeader = `
<!doctype html>
<html>
<head>
<title>Hesfic - {{.Title}}</title>
<link href="//netdna.bootstrapcdn.com/twitter-bootstrap/2.3.1/css/bootstrap-combined.min.css" rel="stylesheet">
<script src="//netdna.bootstrapcdn.com/twitter-bootstrap/2.3.1/js/bootstrap.min.js"></script>
</head>
<body>
<div class="navbar navbar-inverse navbar-fixed-top">
  <div class="navbar-inner">
    <div class="container">
      <a class="brand" href="/">Hesfic</a>
      <ul class="nav">
      <li><a href="/">Snapshots</a></li>
      </ul>
    </div>
  </div>
</div>
<div class="container" style="padding-top: 50px">
`

const commonFooter = `
</table>
</div>
</body>
</html>`

const indexTemplateSrc = commonHeader + `
<h4>{{.Title}}</h4>
<table class="table table-striped table-bordered">
 <tr>
  <th>Date</th>
  <th>Source Path</th>
  <th>Ref</th>
  <th>Comment</th>
 </tr>
 {{range .Snapshots}}
 <tr>
  <td><a href="dir/{{.DirRef}}" title="Snapshot {{.Name}}">{{.Time}}</a></td>
  <td>{{.SourcePath}}</td>
  <td><small style="font: 10px monospace" title="{{.DirRef}}">{{.DirRefPart}}</small></td>
  <td>{{.Comment}}</td>
 </tr>
 {{end}}` + commonFooter

const dirTemplateSrc = commonHeader + `
<h4><a class="btn btn-small" href="javascript:history.back()"><i class="icon-chevron-left"></i></a> &nbsp; Directory <span class="muted">{{.DirRef}}</span></h4>
<table class="table table-bordered">
 <tr>
  <th>Name</th>
  <th>Date Modified</th>
  <th>Size</th>
  <th>Perm.</th>
 </tr>
 {{range .Files}}
 <tr>
  {{if .IsDir}}
  <td><a href="../dir/{{.Ref}}"><i class="icon-folder-close"></i> <b>{{.Name}}</b></a></td>
  {{else}}
  <td><a href="../file/{{.Ref}}"><i class="icon-file"></i> {{.Name}}</a></td>
  {{end}}
  <td>{{.Time}}</td>
  <td>{{.Size}}</td>
  <td>{{.Mode}}</td>
 </tr>
 {{end}}` + commonFooter

const fileTemplateSrc = commonHeader +
	`<h4>Not implemented.</h4>` + commonFooter
