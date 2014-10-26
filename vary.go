package main

import (
	"net/http"
	"net/textproto"
	"sort"
	"strings"
)

func getVaryFields(header http.Header) []string {
	Vary := strings.Replace(strings.Join(header["Vary"], ","), " ", "", -1)
	if strings.Contains(Vary, "*") || len(Vary) > 65535 {
		return []string{"*"}
	}
	if Vary == "" {
		return nil
	}
	fields := strings.Split(Vary, ",")
	for i, field := range fields {
		fields[i] = textproto.CanonicalMIMEHeaderKey(field)
	}
	sort.StringSlice(fields).Sort()
	return fields
}

func getNormalizedHeaders(fields []string, header http.Header) []string {
	res := []string{}
	for _, name := range fields {
		normalized := normalizeHeader(strings.Join(header[name], ","))
		res = append(res, normalized)
	}
	return res
}

func varyHeadersMatch(fields, values []string, header http.Header) bool {
	for i, name := range fields {
		expected := values[i]
		received := normalizeHeader(strings.Join(header[name], ","))
		if received != expected {
			return false
		}
	}
	return true
}
