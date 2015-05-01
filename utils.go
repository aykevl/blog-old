package main

import (
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
