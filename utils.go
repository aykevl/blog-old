package main

import (
	"net/http"
	"strings"
	"time"
)

func importTime(unixTime int64) time.Time {
	if unixTime == 0 {
		return time.Time{}
	}
	return time.Unix(unixTime, 0)
}

func exportTime(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}

func lastTime(times ...time.Time) time.Time {
	var t time.Time

	for _, t2 := range times {
		if t2.After(t) {
			t = t2
		}
	}

	return t
}

func equalLastModified(t time.Time, req *http.Request) bool {
	ims, err := time.Parse(time.RFC1123, req.Header.Get("If-Modified-Since"))
	return err == nil && t.Equal(ims)
	// Error means: there is no IMS header present, or the parser failed.
	//
	// According to rfc7231, an implementation MUST also parse certain
	// legacy date formats.
	// Because I don't think current HTTP clients still use them and a
	// full reply is valid anyway, I'll only parse the recommended date
	// format.
	// See https://tools.ietf.org/html/rfc7231#section-7.1.1.1
}

func httpLastModified(t time.Time) string {
	// We might also use time.RFC1123Z - but the standard appears to require the
	// string "GMT". Go uses "UTC" so we have to replace it (using UTC isn't
	// valid - at least not according to wget).
	return strings.Replace(t.UTC().Format(time.RFC1123), " UTC", " GMT", 1)
}
