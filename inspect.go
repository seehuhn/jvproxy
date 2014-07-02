package main

import (
	"github.com/seehuhn/trace"
	"html/template"
	"net/http"
	"time"
)

var tmplFuncs = template.FuncMap{
	"FormatDate": formatDate,
}

func formatDate(unixNano int64) template.HTML {
	s := time.Unix(0, unixNano).Format("2006-01-02&nbsp;15:04:05.000")
	return template.HTML(s)
}

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

	type tmplData struct {
		ListenAddr string
		Entries    []logEntry
	}
	data := tmplData{
		ListenAddr: listenAddr,
		Entries:    entries,
	}
	err = logTmpl.Execute(w, data)
	if err != nil {
		trace.T("jvproxy/stats", trace.PrioDebug,
			"rendering log entries into template failed: %s", err.Error())
	}
}

var storeTmpl *template.Template

func (s *Store) storeHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/store" {
		http.NotFound(w, r)
		return
	}

	entries := []indexEntry{}
	_, err := s.index.Select(&entries,
		"SELECT * FROM `index` ORDER BY DownloadTimeNano DESC LIMIT 100")
	if err != nil {
		trace.T("jvproxy/stats", trace.PrioDebug,
			"reading store index failed: %s", err.Error())
		http.Error(w, err.Error(), 500)
		return
	}

	type tmplData struct {
		ListenAddr string
		Entries    []indexEntry
	}
	data := tmplData{
		ListenAddr: listenAddr,
		Entries:    entries,
	}
	err = storeTmpl.Execute(w, data)
	if err != nil {
		trace.T("jvproxy/stats", trace.PrioDebug,
			"rendering store index into template failed: %s", err.Error())
	}
}
