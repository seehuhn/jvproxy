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

package jvproxy

import (
	"net/http"
	"strings"

	"github.com/seehuhn/httputil"
)

func parseHeaders(headers []string) (map[string]string, error) {
	res := map[string]string{}
	parts, err := httputil.ParseHeader(strings.Join(headers, ","))
	if err == nil {
		for _, part := range parts {
			if _, ok := res[part.Key]; !ok {
				res[part.Key] = part.Value
			}
		}
	}
	return res, err
}

func copyHeader(dst, src http.Header) {
	for k, vv := range src {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}
