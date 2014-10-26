package main

import (
	"encoding/hex"
	"fmt"
	"github.com/seehuhn/trace"
	"html/template"
	"net/http"
	"path/filepath"
	"time"
)

const tmplDir = "tmpl"

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

var reportTmpl = map[string]*template.Template{}

func installReport(mux *http.ServeMux, name string, handler http.HandlerFunc) {
	tmpl := template.Must(template.New(name+".html").
		Funcs(tmplFuncs).ParseFiles(filepath.Join(tmplDir, name+".html"),
		filepath.Join(tmplDir, "head_frag.html")))
	reportTmpl[name] = tmpl
	url := "/" + name
	mux.HandleFunc(url, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != url {
			http.NotFound(w, r)
			return
		}
		handler(w, r)
	})
}

func installAdminHandlers(mux *http.ServeMux, proxy *Proxy, cache Cache) {
	installReport(mux, "index", func(w http.ResponseWriter, r *http.Request) {
		err := reportTmpl["index"].Execute(w, map[string]interface{}{
			"proxy": proxy,
			"cache": cache,
		})
		if err != nil {
			trace.T("jvproxy/admin", trace.PrioError,
				"rendering summary data into template failed: %s", err.Error())
		}
	})
	mux.Handle("/css/",
		http.StripPrefix("/css/", http.FileServer(http.Dir("css"))))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
		} else {
			http.Redirect(w, r, "/index", http.StatusMovedPermanently)
		}
	})
}
