package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestLocalDefaults(t *testing.T) {
	c, err := FromEnv(env(map[string]string{"HOME": "/home/u"}))
	if err != nil {
		t.Fatal(err)
	}
	if c.Mode != ModeLocal || c.Addr != "127.0.0.1:7878" || c.LogFormat != "text" || c.LogLevel != "info" {
		t.Fatalf("%+v", c)
	}
	if c.DBPath != filepath.Join(c.DataDir, "vaquero.db") {
		t.Fatalf("%q", c.DBPath)
	}
}

func TestDefaultDataDir(t *testing.T) {
	for _, tc := range []struct {
		goos string
		env  map[string]string
		want string
	}{
		{"darwin", map[string]string{"HOME": "/Users/u"}, "/Users/u/Library/Application Support/vaquero"},
		{"linux", map[string]string{"HOME": "/home/u"}, "/home/u/.local/share/vaquero"},
		{"linux", map[string]string{"HOME": "/home/u", "XDG_DATA_HOME": "/x"}, "/x/vaquero"},
	} {
		got, err := DefaultDataDir(tc.goos, env(tc.env))
		if err != nil || got != tc.want {
			t.Fatalf("%s: %q %v", tc.goos, got, err)
		}
	}
	if _, err := DefaultDataDir("linux", env(nil)); err == nil {
		t.Fatal("want error with no HOME")
	}
}

func TestDataDirOverride(t *testing.T) {
	c, err := FromEnv(env(map[string]string{"VQ_DATA_DIR": "/tmp/vq"}))
	if err != nil || c.DBPath != "/tmp/vq/vaquero.db" {
		t.Fatalf("%q %v", c.DBPath, err)
	}
}

// Review Focus 1
func TestLocalRefusesNonLoopbackAddr(t *testing.T) {
	for _, addr := range []string{"0.0.0.0:7878", ":7878", "192.168.1.5:7878", "example.com:7878"} {
		_, err := FromEnv(env(map[string]string{"HOME": "/h", "VQ_ADDR": addr}))
		if err == nil || !strings.Contains(err.Error(), "loopback") {
			t.Fatalf("%s: %v", addr, err)
		}
	}
	for _, addr := range []string{"127.0.0.1:9000", "localhost:9000", "[::1]:9000"} {
		if _, err := FromEnv(env(map[string]string{"HOME": "/h", "VQ_ADDR": addr})); err != nil {
			t.Fatalf("%s: %v", addr, err)
		}
	}
}

func TestHostedNotYetAvailable(t *testing.T) {
	_, err := FromEnv(env(map[string]string{"HOME": "/h", "VQ_MODE": "hosted"}))
	if err == nil || !strings.Contains(err.Error(), "not available yet") {
		t.Fatalf("%v", err)
	}
}

func TestBadValuesRejected(t *testing.T) {
	for k, v := range map[string]string{"VQ_MODE": "cloud", "VQ_LOG_FORMAT": "xml", "VQ_ADDR": "nope"} {
		if _, err := FromEnv(env(map[string]string{"HOME": "/h", k: v})); err == nil {
			t.Fatalf("%s=%s accepted", k, v)
		}
	}
}

func TestAllowedHosts(t *testing.T) {
	c, err := FromEnv(env(map[string]string{"HOME": "/h"}))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(c.AllowedHosts(), ","); got != "localhost,127.0.0.1,::1" {
		t.Fatalf("%q", got)
	}
}

func TestBlobConfigDefaults(t *testing.T) {
	c, err := FromEnv(env(map[string]string{"VQ_DATA_DIR": "/d"}))
	if err != nil {
		t.Fatal(err)
	}
	if c.BlobBackend != "sqlite" || c.BlobDir != filepath.Join("/d", "attachments") || c.S3Region != "auto" {
		t.Fatalf("%+v", c)
	}
}

func TestBlobConfigS3Missing(t *testing.T) {
	_, err := FromEnv(env(map[string]string{"VQ_DATA_DIR": "/d", "VQ_BLOB_STORE": "s3"}))
	if err == nil {
		t.Fatal("want an error")
	}
	for _, v := range []string{"VQ_S3_ENDPOINT", "VQ_S3_BUCKET", "VQ_S3_ACCESS_KEY_ID", "VQ_S3_SECRET_ACCESS_KEY"} {
		if !strings.Contains(err.Error(), v) {
			t.Fatalf("%q not named in %v", v, err)
		}
	}
}

func TestBlobConfigS3Complete(t *testing.T) {
	c, err := FromEnv(env(map[string]string{"VQ_DATA_DIR": "/d", "VQ_BLOB_STORE": "S3",
		"VQ_S3_ENDPOINT": "s3.example.com", "VQ_S3_BUCKET": "b", "VQ_S3_ACCESS_KEY_ID": "k", "VQ_S3_SECRET_ACCESS_KEY": "s"}))
	if err != nil {
		t.Fatal(err)
	}
	if c.BlobBackend != "s3" || string(c.S3SecretAccessKey) != "s" || c.S3Region != "auto" {
		t.Fatalf("%+v", c)
	}
}

func TestBlobConfigUnknown(t *testing.T) {
	_, err := FromEnv(env(map[string]string{"VQ_DATA_DIR": "/d", "VQ_BLOB_STORE": "ftp"}))
	if err == nil || !strings.Contains(err.Error(), "VQ_BLOB_STORE") {
		t.Fatalf("%v", err)
	}
}
