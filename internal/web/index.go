package web

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/neverbot/owl/internal/dashboards"
)

// serveIndex server-renders the homepage listing all known dashboards.
func (s *Server) serveIndex(w http.ResponseWriter, r *http.Request) {
	var list []*dashboards.Dashboard
	if s.opt.Loader != nil {
		list = s.opt.Loader.List()
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = fmt.Fprint(w, indexHead)

	if len(list) == 0 {
		_, _ = fmt.Fprintf(w, `<p class="hint">No dashboards found. Drop a Grafana JSON file into the <code>dashboards/</code> directory configured as <code>dashboards.dir</code>.</p>`)
	} else {
		var sb strings.Builder
		sb.WriteString(`<ul class="dashboard-list">`)
		for _, d := range list {
			sb.WriteString(fmt.Sprintf(`<li><a href="/d/%s">%s</a> <span class="hint">(%d panels)</span></li>`,
				d.ID, d.Title, len(d.Panels)))
		}
		sb.WriteString(`</ul>`)
		_, _ = fmt.Fprint(w, sb.String())
	}

	_, _ = fmt.Fprint(w, indexTail)
}

const indexHead = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>owl</title>
  <link rel="stylesheet" href="/static/owl.css">
  <style>.dashboard-list{list-style:none;padding:0;margin:0}.dashboard-list li{margin-bottom:0.75rem}.dashboard-list a{font-size:1.125rem;font-weight:500;text-decoration:none;color:var(--fg)}.dashboard-list a:hover{text-decoration:underline}</style>
</head>
<body>
  <main>
    <h1>owl</h1>
    <p class="hint">Dashboards</p>
`

const indexTail = `
  </main>
</body>
</html>
`
