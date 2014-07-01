package main

import (
	"github.com/seehuhn/trace"
	"html/template"
	"net/http"
)

var logTmpl *template.Template

func (s *Store) logHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/log" {
		http.NotFound(w, r)
		return
	}

	entries := []logEntry{}
	_, err := s.index.Select(&entries,
		"SELECT * FROM log ORDER BY RequestTimeNano DESC LIMIT 100")
	if err != nil {
		trace.T("jvproxy/stats", trace.PrioDebug,
			"reading log entries failed: %s", err.Error())
		http.Error(w, err.Error(), 500)
		return
	}

	err = logTmpl.Execute(w, entries)
	if err != nil {
		trace.T("jvproxy/stats", trace.PrioDebug,
			"rendering log entries into template failed: %s", err.Error())
	}
}
