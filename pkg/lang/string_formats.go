package lang

import (
	"encoding/base64"
	"encoding/json"
	"net"
	"regexp"
	"strconv"
	"time"
)

// StringFormat looks up a string validator by its format name.
func StringFormat(name string) (func(string) bool, bool) {
	switch name {
	case "regex":
		return func(s string) bool { _, err := regexp.Compile(s); return err == nil }, true
	case "ip":
		return func(s string) bool { return net.ParseIP(s) != nil }, true
	case "cidr":
		return func(s string) bool { _, _, err := net.ParseCIDR(s); return err == nil }, true
	case "base64":
		return func(s string) bool {
			_, err := base64.StdEncoding.DecodeString(s)
			return err == nil
		}, true
	case "positive-duration":
		return func(s string) bool {
			d, err := time.ParseDuration(s)
			return err == nil && d > 0
		}, true
	case "date-time":
		return func(s string) bool { return validateDateTime(s) == nil }, true
	case "json":
		return func(s string) bool { return json.Valid([]byte(s)) }, true
	case "int64":
		return func(s string) bool { _, err := strconv.ParseInt(s, 10, 64); return err == nil }, true
	default:
		return nil, false
	}
}

func validateDateTime(s string) error {
	_, err := time.Parse(time.RFC3339, s)
	return err
}
