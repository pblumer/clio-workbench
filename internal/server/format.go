package server

import (
	"fmt"
	"strconv"
)

// thousands renders an integer count with comma thousands separators so the
// large event/subject counts stay legible at a glance (50000 → "50,000"). It is
// exposed to templates as the `thousands` func and accepts the int and int64
// counts the views carry (EventsTotal is int64, the rest int). Anything else is
// printed verbatim, so a misuse degrades gracefully rather than panicking.
func thousands(v any) string {
	var n int64
	switch x := v.(type) {
	case int:
		n = int64(x)
	case int64:
		n = x
	case int32:
		n = int64(x)
	default:
		return fmt.Sprint(v)
	}

	neg := n < 0
	if neg {
		n = -n
	}
	digits := strconv.FormatInt(n, 10)

	// Walk the digits right-to-left, inserting a comma every three.
	var b []byte
	for i, c := range []byte(digits) {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b = append(b, ',')
		}
		b = append(b, c)
	}
	if neg {
		return "-" + string(b)
	}
	return string(b)
}
