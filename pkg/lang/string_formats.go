package lang

import (
	"encoding/base64"
	"encoding/json"
	"net"
	"regexp"
	"strconv"
	"time"

	"github.com/cloudboss/unobin/pkg/constraint"
)

// StringFormat looks up a string validator by its format name.
func StringFormat(name string) (func(string) bool, bool) {
	switch name {
	case constraint.FormatRegex:
		return func(s string) bool { _, err := regexp.Compile(s); return err == nil }, true
	case constraint.FormatIP:
		return func(s string) bool { return net.ParseIP(s) != nil }, true
	case constraint.FormatCIDR:
		return func(s string) bool { _, _, err := net.ParseCIDR(s); return err == nil }, true
	case constraint.FormatBase64:
		return func(s string) bool {
			_, err := base64.StdEncoding.DecodeString(s)
			return err == nil
		}, true
	case constraint.FormatPositiveDuration:
		return func(s string) bool {
			d, err := time.ParseDuration(s)
			return err == nil && d > 0
		}, true
	case constraint.FormatDateTime:
		return func(s string) bool { return validateDateTime(s) == nil }, true
	case constraint.FormatJSON:
		return func(s string) bool { return json.Valid([]byte(s)) }, true
	case constraint.FormatInt64:
		return func(s string) bool { _, err := strconv.ParseInt(s, 10, 64); return err == nil }, true
	default:
		return nil, false
	}
}

func validateDateTime(s string) error {
	_, err := time.Parse(time.RFC3339, s)
	return err
}
