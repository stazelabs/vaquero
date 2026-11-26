// Package configtest holds the sentinel secret the leak tests share
// (TestRedactNeverLeaks in cmd/vaquero; the admin page's in the handlers):
// a surface passes when neither the secret nor any long part of it shows.
package configtest

import (
	"strings"
	"testing"
)

// Secret is a sentinel S3 secret, 40 characters like a real one. A test
// sets it as VQ_S3_SECRET_ACCESS_KEY and checks every surface with NoLeak.
const Secret = "SENTINELsecretXq7Zp2Lk9Wm4Rt6Yv8Bn3Hj5Dc"

// AccessKeyID is a sentinel access key id, 20 characters.
const AccessKeyID = "AKIASENTINELKEYID7Q2"

// NoLeak fails t when text, from the named surface, holds the secret, its
// first 12 characters, its last 12, or the access key id whole. The last 4
// characters alone may show: that is what config.Redact gives.
func NoLeak(t testing.TB, surface, text string) {
	t.Helper()
	for _, part := range []string{Secret, Secret[:12], Secret[len(Secret)-12:], AccessKeyID} {
		if strings.Contains(text, part) {
			t.Errorf("%s shows %q:\n%s", surface, part, text)
		}
	}
}
