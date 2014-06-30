// headers.go - process HTTP headers
// Copyright (C) 2014  Jochen Voss <voss@seehuhn.de>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package main

import (
	"errors"
	"github.com/seehuhn/trace"
	"net/http"
	"strings"
	"unicode/utf8"
)

var (
	ErrMissingComma               = errors.New("missing comma")
	ErrUnexpectedControlCharacter = errors.New("unexpected control character")
	ErrUnexpectedQuotedString     = errors.New("unexpected quoted string")
	ErrUnterminatedEscape         = errors.New("unterminated escape")
	ErrUnterminatedString         = errors.New("unterminated string")
)

func isTSpecial(c rune) bool {
	for _, d := range "()<>@,;:\\\"/[]?={} \t" {
		if c == d {
			return true
		}
	}
	return false
}

func isCtl(c rune) bool {
	return c < 32 || c == 127
}

func tokenizeHeader(header string) ([]string, error) {
	res := []string{}

	start := 0
	quoted := false
	escaped := false

	runes := []rune(header)
	for pos, c := range runes {
		if quoted {
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				quoted = false
				res = append(res, string(runes[start:pos+1]))
				start = pos + 1
			}
		} else if isTSpecial(c) {
			if pos > start {
				res = append(res, string(runes[start:pos]))
			}

			start = pos + 1
			if c == '"' {
				quoted = true
				start = pos
			} else if c != ' ' && c != '\t' {
				res = append(res, string(runes[pos:pos+1]))
			}
		} else if isCtl(c) {
			return nil, ErrUnexpectedControlCharacter
		}
	}

	if escaped {
		return nil, ErrUnterminatedEscape
	} else if quoted {
		return nil, ErrUnterminatedString
	} else if start < len(runes) {
		res = append(res, string(runes[start:]))
	}

	return res, nil
}

func normalizeHeader(header string) string {
	res := ""
	tokens, err := tokenizeHeader(header)
	if err != nil {
		return header
	}
	for _, token := range tokens {
		t, _ := utf8.DecodeRuneInString(token)
		if t == utf8.RuneError {
			return header
		}
		if res == "" || isTSpecial(t) {
			res += token
		} else {
			t, _ = utf8.DecodeLastRuneInString(token)
			if t == utf8.RuneError {
				return header
			}
			if isTSpecial(t) {
				res += token
			} else {
				res += " " + token
			}
		}
	}
	return res
}

type headerPart struct {
	Key, Value string
}

func parseHeader(header string) ([]headerPart, error) {
	tokens, err := tokenizeHeader(header)
	if err != nil {
		return nil, err
	}

	res := []headerPart{}

	requireComma := false

	i := 0
	n := len(tokens)
	for i < n {
		if tokens[i] == "," {
			i++
			requireComma = false
			continue
		}
		if requireComma {
			return nil, ErrMissingComma
		}
		if tokens[i][0] == '"' {
			return nil, ErrUnexpectedQuotedString
		}

		part := headerPart{Key: tokens[i]}
		i++
		if i+1 < n && tokens[i] == "=" {
			part.Value = tokens[i+1]
			i += 2
		}
		res = append(res, part)
		requireComma = true
	}

	return res, nil
}

func parseHeaders(headers []string) (map[string]string, error) {
	res := map[string]string{}
	parts, err := parseHeader(strings.Join(headers, ","))
	if err == nil {
		for _, part := range parts {
			if _, ok := res[part.Key]; !ok {
				res[part.Key] = part.Value
			}
		}
	}
	return res, err
}

// first result: can use cache for response
// second result: can store server response in cache
func canUseCache(headers http.Header, log *logEntry) (bool, bool) {
	parts, err := parseHeaders(headers["Pragma"])
	if err != nil {
		trace.T("jvproxy/handler", trace.PrioDebug,
			"cannot parse request Pragma directive: %s", err.Error())
	}
	if _, ok := parts["no-cache"]; ok {
		log.Comment += "Pragma:no-cache "
		return false, true
	}

	parts, err = parseHeaders(headers["Cache-Control"])
	if err != nil {
		trace.T("jvproxy/handler", trace.PrioDebug,
			"cannot parse request Cache-Control directive: %s", err.Error())
	}
	if _, ok := parts["no-cache"]; ok {
		log.Comment += "CC:no-cache "
		return false, true
	}
	if _, ok := parts["no-store"]; ok {
		log.Comment += "CC:no-store "
		return false, false
	}

	return true, true
}

func canStoreResponse() bool {
	// parts, err := parseHeaders(headers["Pragma"])
	// if err != nil {
	//	trace.T("jvproxy/handler", trace.PrioDebug,
	//		"cannot parse Pragma directive: %s", err.Error())
	// }
	// if _, ok := parts["no-cache"]; ok {
	//	cacheable = false
	// }

	// parts, err = parseHeaders(headers["Cache-Control"])
	// if err != nil {
	//	trace.T("jvproxy/handler", trace.PrioDebug,
	//		"cannot parse Cache-Control directive: %s", err.Error())
	// }
	// if _, ok := parts["public"]; ok {
	//	cacheable = false
	// }
	// if _, ok := parts["public"]; ok {
	//	cacheable = true
	// }
	// if _, ok := parts["no-cache"]; ok {
	//	// TODO(voss): what to do if field names are specified?
	//	cacheable = false
	// }
	// if _, ok := parts["no-store"]; ok {
	//	cacheable = false
	// }

	return true
}
