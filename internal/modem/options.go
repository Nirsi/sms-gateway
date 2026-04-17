package modem

import (
	"strconv"
	"strings"
)

// Options are passed to a driver Factory when constructing a Modem.
//
// Port and BaudRate are common to any serial-based driver. Extra carries
// driver-specific key=value settings; each driver documents the keys it
// understands and ignores unknown ones.
type Options struct {
	Port     string
	BaudRate int
	Extra    map[string]string
}

// String returns the Extra value for key, or def if not set.
func (o Options) String(key, def string) string {
	if o.Extra == nil {
		return def
	}
	v, ok := o.Extra[key]
	if !ok {
		return def
	}
	return v
}

// Int returns the Extra value for key parsed as an int, or def on miss or
// parse error. Malformed values (e.g. "abc", "12x") silently fall back to def.
func (o Options) Int(key string, def int) int {
	v := o.String(key, "")
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

// Bool returns the Extra value for key parsed as a bool, or def on miss.
// Accepts: 1/0, true/false, yes/no, on/off (case-insensitive). Unknown
// values (e.g. "maybe", "2") silently fall back to def.
func (o Options) Bool(key string, def bool) bool {
	v := strings.ToLower(o.String(key, ""))
	switch v {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return def
}
