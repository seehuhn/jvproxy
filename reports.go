package main

import (
	"crypto/sha256"
	"encoding/gob"
	"encoding/hex"
	"fmt"
	"github.com/seehuhn/trace"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

var tmplFuncs = template.FuncMap{
	"FormatDate":          formatDate,
	"FormatDateNano":      formatDateNano,
	"FormatTimeDelta":     formatTimeDelta,
	"FormatTimeDeltaNano": formatTimeDeltaNano,
	"BytesToHex":          bytesToHex,
}

func formatDate(unix int64) template.HTML {
	s := "&mdash;"
	if unix > 0 {
		t := time.Unix(unix, 0)
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

func formatTimeDelta(unix int64) template.HTML {
	if unix <= 0 {
		return template.HTML("&mdash;")
	}
	return doFormatTimeDelta(time.Unix(unix, 0))
}

func formatTimeDeltaNano(unixNano int64) template.HTML {
	t := time.Unix(0, unixNano)
	return doFormatTimeDelta(t)
}

func doFormatTimeDelta(t time.Time) template.HTML {
	s := "&mdash;"
	if !t.IsZero() {
		d := t.Sub(time.Now())
		if d < 0 {
			s = "-"
			d = -d
		} else {
			s = ""
		}
		if d >= 24*time.Hour {
			hours := int(d / time.Hour)
			s += fmt.Sprintf("%dd%02dh", hours/24, hours%24)
		} else if d >= time.Hour {
			minutes := int(d / time.Minute)
			s += fmt.Sprintf("%dh%02dm", minutes/60, minutes%60)
		} else if d >= time.Minute {
			seconds := int(d / time.Second)
			s += fmt.Sprintf("%dm%02ds", seconds/60, seconds%60)
		} else if d > 0 {
			s += fmt.Sprintf("%.2fs", float64(d)/float64(time.Second))
		} else {
			s = "0"
		}
	}
	return template.HTML(s)
}

func bytesToHex(x []byte) template.HTML {
	return template.HTML(hex.EncodeToString(x))
}

var reportMux = http.NewServeMux() // TODO(voss): make this Cache-local
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
		"SELECT SUM(ContentLength) FROM \"index\"")

	err = reportTmpl["summary"].Execute(w, summaryData)
	if err != nil {
		trace.T("jvproxy/stats", trace.PrioDebug,
			"rendering summary data into template failed: %s", err.Error())
	}
}

func (s *Store) logHandler(w http.ResponseWriter, r *http.Request) {
	param := r.URL.Query()
	n, _ := strconv.Atoi(param.Get("n"))
	if n <= 0 {
		n = 100
	}
	pos, _ := strconv.Atoi(param.Get("pos"))

	entries := []logEntry{}
	_, err := s.index.Select(&entries,
		"SELECT * FROM log ORDER BY RequestTimeNano DESC LIMIT ?, ?", pos, n)
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
		"SELECT * FROM \"index\" ORDER BY DownloadTimeNano DESC LIMIT 100")
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

	entries := []*indexEntry{}
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
		Entries    []*indexEntry
		Vary       [][]template.HTML
		VaryHash   []string
	}
	data := tmplData{
		ListenAddr: listenAddr,
		UrlPath:    queryUrl,
		Entries:    entries,
	}

	varHeaderMap := map[string]bool{}
	for _, entry := range entries {
		entryHeaders := []string{}
		if entry.Vary != "" {
			entryHeaders = strings.Split(entry.Vary, ",")
		}
		for _, h := range entryHeaders {
			varHeaderMap[h] = true
		}
	}
	data.Headers = []string{}
	for h, _ := range varHeaderMap {
		data.Headers = append(data.Headers, h)
	}
	sort.Strings(data.Headers)

	if len(data.Headers) > 0 {
		shortHash := map[string]string{}
		nextShortHash := 'A'
		for _, entry := range entries {
			fname := "xxx" // TODO(voss): store.fileName(entry.Id)
			f, _ := os.Open(fname + "c")
			dec := gob.NewDecoder(f)
			header := http.Header{}
			dec.Decode(&header)
			f.Close()

			entryHeaders := []string{}
			if entry.Vary != "" {
				entryHeaders = strings.Split(entry.Vary, ",")
			}

			varyData := []template.HTML{}
			for _, h := range data.Headers {
				var x string
				if stringInSlice(h, entryHeaders) {
					val, ok := header[h]
					if ok {
						x = template.HTMLEscapeString(strings.Join(val, ", "))
					} else {
						x = "<i>not recorded</i>"
					}
				} else {
					x = "&mdash;"
				}
				varyData = append(varyData, template.HTML(x))
			}
			data.Vary = append(data.Vary, varyData)

			key := string(entry.VaryHash)
			short, ok := shortHash[key]
			if !ok {
				short = string(nextShortHash)
				shortHash[key] = short
				nextShortHash++
			}
			data.VaryHash = append(data.VaryHash, short)
		}
	}

	err = reportTmpl["variants"].Execute(w, data)
	if err != nil {
		trace.T("jvproxy/stats", trace.PrioDebug,
			"rendering store index into template failed: %s", err.Error())
	}
}
