package config

import (
	"strings"
	"testing"
)

func TestRedact(t *testing.T) {
	secret := strings.Repeat("x", 36) + "abcd"
	if got := Redact("VQ_S3_SECRET_ACCESS_KEY", secret); got != "••••••••abcd" {
		t.Fatalf("%q", got)
	}
	if got := Redact("VQ_S3_SECRET_ACCESS_KEY", "0123456789"); got != "set" {
		t.Fatalf("%q", got)
	}
	if got := Redact("VQ_S3_SECRET_ACCESS_KEY", strings.Repeat("y", 15)); got != "set" {
		t.Fatalf("15: %q", got)
	}
	if got := Redact("VQ_S3_SECRET_ACCESS_KEY", strings.Repeat("y", 12)+"wxyz"); got != "••••••••wxyz" {
		t.Fatalf("16: %q", got)
	}
	if got := Redact("VQ_S3_ACCESS_KEY_ID", "AKIAIOSFODNN7EXAMPLE"); got != "AKIA…MPLE" {
		t.Fatalf("%q", got)
	}
	if got := Redact("VQ_S3_ACCESS_KEY_ID", "AKIA1234567"); got != "set" {
		t.Fatalf("11: %q", got)
	}
	if got := Redact("VQ_S3_ACCESS_KEY_ID", "AKIA12345678"); got != "AKIA…5678" {
		t.Fatalf("12: %q", got)
	}
	if got := Redact("VQ_BLOB_STORE", "s3"); got != "s3" {
		t.Fatalf("%q", got)
	}
	for _, k := range []string{"VQ_S3_SECRET_ACCESS_KEY", "VQ_S3_ACCESS_KEY_ID"} {
		if got := Redact(k, ""); got != "" {
			t.Fatalf("%s unset: %q", k, got)
		}
	}
}
