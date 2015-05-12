package jvproxy

import (
	"github.com/seehuhn/httputil"
	"github.com/seehuhn/jvproxy/cache"
	"strconv"
	"time"
)

func (proxy *Proxy) getFreshnessLifetime(entry *cache.Entry) time.Duration {
	// see http://tools.ietf.org/html/rfc7234#section-4.2.1
	res := -3600 * 24 * 365 * time.Second
	cc, _ := parseHeaders(entry.Header["Cache-Control"])
	if sMaxAge, hasSMaxAge := cc["s-maxage"]; hasSMaxAge && proxy.shared {
		ageSec, err := strconv.Atoi(sMaxAge)
		if err == nil {
			res = time.Duration(ageSec) * time.Second
		}
	} else if maxAge, hasMaxAge := cc["max-age"]; hasMaxAge {
		ageSec, err := strconv.Atoi(maxAge)
		if err == nil {
			res = time.Duration(ageSec) * time.Second
		}
	} else if expires, hasExpires := entry.Header["Expires"]; hasExpires {
		date := entry.Header["Date"]
		if len(expires) == 1 && len(date) == 1 {
			a := httputil.ParseDate(expires[0])
			b := httputil.ParseDate(date[0])
			if !a.IsZero() && !b.IsZero() {
				res = a.Sub(b)
			}
		}
	}
	return res
}

func (proxy *Proxy) getCurrentAge(entry *cache.Entry) time.Duration {
	// see http://tools.ietf.org/html/rfc7234#section-4.2.3
	res := 3600 * 24 * 365 * time.Second

	dateValue := httputil.ParseDate(entry.Header.Get("Date"))
	if !dateValue.IsZero() {
		age, _ := strconv.Atoi(entry.Header.Get("Age"))
		ageValue := time.Duration(age) * time.Second
		correctedInitialAge := ageValue + entry.ResponseDelay

		apparentAge := entry.ResponseTime.Sub(dateValue)
		if apparentAge < 0*time.Second {
			apparentAge = 0 * time.Second
		}
		if apparentAge > correctedInitialAge {
			correctedInitialAge = apparentAge
		}

		residentTime := time.Since(entry.ResponseTime)
		res = correctedInitialAge + residentTime
	}

	return res
}
