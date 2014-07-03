package main

import (
	"crypto/sha256"
	"github.com/seehuhn/trace"
	"html/template"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var tmplFuncs = template.FuncMap{
	"FormatDate":     formatDate,
	"FormatDateNano": formatDateNano,
}

func formatDate(unixNano int64) template.HTML {
	t := time.Unix(unixNano, 0)
	s := "&mdash;"
	if !t.IsZero() {
		s = t.Format("2006-01-02&nbsp;15:04:05")
	}
	return template.HTML(s)
}

func formatDateNano(unixNano int64) template.HTML {
	t := time.Unix(0, unixNano)
	s := "&mdash;"
	if !t.IsZero() {
		s = t.Format("2006-01-02&nbsp;15:04:05.000")
	}
	return template.HTML(s)
}

var reportMux = http.NewServeMux()
var reportTmpl = map[string]*template.Template{}

func installReport(name string, handler http.HandlerFunc) {
	tmpl := template.Must(template.New(name+".html").
		Funcs(tmplFuncs).ParseFiles(filepath.Join("tmpl", name+".html"),
		filepath.Join("tmpl", "head_frag.html")))
	reportTmpl[name] = tmpl
	url := "/" + name
	reportMux.HandleFunc(url, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != url {
			http.NotFound(w, r)
			return
		}
		handler(w, r)
	})
}

func (s *Store) summaryHandler(w http.ResponseWriter, r *http.Request) {
	var summaryData struct {
		ListenAddr string
		Log        []struct {
			CacheResult string
			TotalCount  int
			TotalSize   int64
		}
		StoreTotal int64
	}

	summaryData.ListenAddr = listenAddr

	_, err := s.index.Select(&summaryData.Log,
		`SELECT CacheResult, COUNT(*) as TotalCount,
				SUM(ContentLength) AS TotalSize FROM log
		 GROUP BY CacheResult;`)
	if err != nil {
		trace.T("jvproxy/stats", trace.PrioDebug,
			"reading summary data failed: %s", err.Error())
		http.Error(w, err.Error(), 500)
		return
	}

	summaryData.StoreTotal, err = s.index.SelectInt(
		"SELECT SUM(ContentLength) FROM `index`")

	err = reportTmpl["summary"].Execute(w, summaryData)
	if err != nil {
		trace.T("jvproxy/stats", trace.PrioDebug,
			"rendering summary data into template failed: %s", err.Error())
	}
}

func (s *Store) logHandler(w http.ResponseWriter, r *http.Request) {
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
	err = reportTmpl["log"].Execute(w, data)
	if err != nil {
		trace.T("jvproxy/stats", trace.PrioDebug,
			"rendering log entries into template failed: %s", err.Error())
	}
}

func (s *Store) storeHandler(w http.ResponseWriter, r *http.Request) {
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
	err = reportTmpl["store"].Execute(w, data)
	if err != nil {
		trace.T("jvproxy/stats", trace.PrioDebug,
			"rendering store index into template failed: %s", err.Error())
	}
}

func (s *Store) highlightsHandler(w http.ResponseWriter, r *http.Request) {
	candidates := []struct {
		RequestURI string
		Count      int
		Size       int64
	}{}
	_, err := s.index.Select(&candidates,
		`SELECT RequestURI, COUNT(*) AS Count, SUM(ContentLength) AS Size
		 FROM log
		 WHERE CacheResult != "HIT"
		 GROUP BY RequestURI
		 HAVING Count>1
		 ORDER BY Size DESC
		 LIMIT 10`)
	if err != nil {
		trace.T("jvproxy/stats", trace.PrioDebug,
			"reading highlight candidates failed: %s", err.Error())
		http.Error(w, err.Error(), 500)
		return
	}

	type candiData struct {
		RequestURI string
		Requests   []struct {
			Method        string
			StatusCode    int
			ContentLength int64
			CacheResult   string
			Comment       string
		}
	}
	type tmplData struct {
		ListenAddr string
		Entries    []candiData
	}
	data := tmplData{
		ListenAddr: listenAddr,
	}
	for _, cand := range candidates {
		cData := candiData{
			RequestURI: cand.RequestURI,
		}
		_, err = s.index.Select(&cData.Requests,
			`SELECT Method, StatusCode, ContentLength, CacheResult, Comment
			 FROM log
			 WHERE RequestURI=?
			 ORDER BY RequestTimeNano DESC`, cand.RequestURI)
		if err != nil {
			trace.T("jvproxy/stats", trace.PrioDebug,
				"reading highlight entries failed: %s", err.Error())
			http.Error(w, err.Error(), 500)
			return
		}
		data.Entries = append(data.Entries, cData)
	}
	err = reportTmpl["highlights"].Execute(w, data)
	if err != nil {
		trace.T("jvproxy/stats", trace.PrioDebug,
			"rendering highlights into template failed: %s", err.Error())
	}
}

func stringInSlice(needle string, haystack []string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

func (s *Store) variantsHandler(w http.ResponseWriter, r *http.Request) {
	queryUrl := r.URL.Query().Get("url")
	if queryUrl == "" {
		http.Error(w, "missing query parameter 'url'", http.StatusNotFound)
		return
	}
	urlHash := sha256.Sum224([]byte(queryUrl))

	entries := []indexEntry{}
	_, err := s.index.Select(&entries,
		`SELECT * FROM [index]
		 WHERE Hash=?
		 ORDER BY DownloadTimeNano DESC
		 LIMIT 100`,
		urlHash[:])
	if err != nil {
		trace.T("jvproxy/stats", trace.PrioDebug,
			"reading store index failed: %s", err.Error())
		http.Error(w, err.Error(), 500)
		return
	}

	type tmplData struct {
		ListenAddr string
		UrlPath    string
		Headers    []string
		Entries    []indexEntry
		Vary       [][]template.HTML
	}
	data := tmplData{
		ListenAddr: listenAddr,
		UrlPath:    queryUrl,
		Entries:    entries,
	}

	varHeaderMap := map[string]bool{}
	for _, entry := range entries {
		entryHeaders := strings.Split(entry.Vary, ",")
		for _, h := range entryHeaders {
			varHeaderMap[h] = true
		}
	}
	data.Headers = []string{}
	for h, _ := range varHeaderMap {
		data.Headers = append(data.Headers, h)
	}
	sort.Strings(data.Headers)

	for _, entry := range entries {
		entryHeaders := strings.Split(entry.Vary, ",")
		varyData := []template.HTML{}
		for _, h := range data.Headers {
			var x string
			if stringInSlice(h, entryHeaders) {
				x = "must match"
			} else {
				x = "&mdash;"
			}
			varyData = append(varyData, template.HTML(x))
		}
		data.Vary = append(data.Vary, varyData)
	}

	err = reportTmpl["variants"].Execute(w, data)
	if err != nil {
		trace.T("jvproxy/stats", trace.PrioDebug,
			"rendering store index into template failed: %s", err.Error())
	}
}
