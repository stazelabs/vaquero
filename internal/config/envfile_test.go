package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// writeEnv writes dir/vaquero.env at mode 0600.
func writeEnv(t *testing.T, dir, text string) string {
	t.Helper()
	path := filepath.Join(dir, EnvFileName)
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// P-R4's lines: KEY=value with optional double quotes, # comments, blank
// lines; an unknown VQ_ key is a warning; anything else is refused.
func TestEnvFileFormat(t *testing.T) {
	dir := t.TempDir()
	writeEnv(t, dir, `# storage for the brew service

VQ_BLOB_STORE=dir
VQ_BLOB_DIR="/srv/vaquero files"
  VQ_LOG_LEVEL = debug  
VQ_S3_SECRET_ACCESS_KEY=ab=cd#ef
VQ_ADDR="127.0.0.1:7900"
VQ_ADDR=127.0.0.1:7901
VQ_FUTURE_THING=1
`)
	got, err := ReadEnvFile(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"VQ_BLOB_STORE": "dir", "VQ_BLOB_DIR": "/srv/vaquero files", "VQ_LOG_LEVEL": "debug",
		"VQ_S3_SECRET_ACCESS_KEY": "ab=cd#ef", "VQ_ADDR": "127.0.0.1:7901", "VQ_FUTURE_THING": "1",
	}
	if len(got) != len(want) {
		t.Fatalf("%v", got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("%s = %q, want %q", k, got[k], v)
		}
	}
	c, err := FromEnv(env(map[string]string{"VQ_DATA_DIR": dir}))
	if err != nil {
		t.Fatal(err)
	}
	if c.BlobBackend != "dir" || c.BlobDir != "/srv/vaquero files" || c.LogLevel != "debug" || c.Addr != "127.0.0.1:7901" {
		t.Fatalf("%+v", c)
	}
	if !slices.Equal(c.Warnings, []string{"vaquero.env sets VQ_FUTURE_THING, which Vaquero doesn't read."}) {
		t.Fatalf("warnings %q", c.Warnings)
	}

	for text, msg := range map[string]string{
		"VQ_ADDR=1\nnonsense\n":          "Line 2 of " + filepath.Join(dir, EnvFileName) + " isn't a KEY=value setting.",
		"=x\n":                           "isn't a KEY=value setting.",
		"PATH=/usr/bin\n":                "Line 1 of " + filepath.Join(dir, EnvFileName) + " sets PATH; vaquero.env takes only VQ_ settings.",
		"export VQ_ADDR=127.0.0.1:1\n":   "Line 1 of " + filepath.Join(dir, EnvFileName) + " doesn't set a VQ_ setting.",
		"vq_addr=127.0.0.1:1\n":          "doesn't set a VQ_ setting.",
		"VQ_S3_SECRET_ACCESS_KEY\n":      "isn't a KEY=value setting.",
		"VQ_S3_REGION=x\nVQ_DATA_DIR=/x": "VQ_DATA_DIR can't be set in vaquero.env.",
	} {
		writeEnv(t, dir, text)
		_, err := ReadEnvFile(dir)
		if err == nil || !strings.Contains(err.Error(), msg) {
			t.Fatalf("%q: %v", text, err)
		}
	}
	// A pasted secret (base64 with padding) is never quoted back.
	writeEnv(t, dir, "wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY==\n")
	_, err = ReadEnvFile(dir)
	if err == nil || err.Error() != "Line 1 of "+filepath.Join(dir, EnvFileName)+" doesn't set a VQ_ setting." {
		t.Fatalf("%v", err)
	}
	// A missing file is no settings.
	if got, err := ReadEnvFile(t.TempDir()); err != nil || len(got) != 0 {
		t.Fatalf("%v %v", got, err)
	}
}

func TestEnvFileRefusesDataDir(t *testing.T) {
	dir := t.TempDir()
	writeEnv(t, dir, "VQ_DATA_DIR="+t.TempDir()+"\n")
	for name, load := range loaders() {
		_, err := load(env(map[string]string{"VQ_DATA_DIR": dir}))
		if err == nil || !strings.Contains(err.Error(), "VQ_DATA_DIR can't be set in vaquero.env.") {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

// loaders are the three ways a command reads its settings.
func loaders() map[string]func(func(string) string) (Config, error) {
	return map[string]func(func(string) string) (Config, error){
		"FromEnv": FromEnv, "Local": Local, "LocalWithBlobs": LocalWithBlobs,
	}
}

func TestEnvFileModeRefused(t *testing.T) {
	dir := t.TempDir()
	path := writeEnv(t, dir, "VQ_BLOB_STORE=dir\n")
	for _, mode := range []os.FileMode{0o644, 0o640, 0o604} {
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
		want := "vaquero.env can be read by other users; run chmod 600 " + path + "."
		for name, load := range loaders() {
			_, err := load(env(map[string]string{"VQ_DATA_DIR": dir}))
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("%o %s: %v", mode, name, err)
			}
		}
	}
	// P-R6: one others can change is refused too, read bits named first.
	for _, mode := range []os.FileMode{0o622, 0o602, 0o620} {
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
		want := "vaquero.env can be changed by other users; run chmod 600 " + path + "."
		for name, load := range loaders() {
			_, err := load(env(map[string]string{"VQ_DATA_DIR": dir}))
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("%o %s: %v", mode, name, err)
			}
		}
	}
	if err := os.Chmod(path, 0o666); err != nil {
		t.Fatal(err)
	}
	if _, err := Local(env(map[string]string{"VQ_DATA_DIR": dir})); err == nil || !strings.Contains(err.Error(), "can be read by other users") {
		t.Fatalf("0666: %v", err)
	}
	for _, mode := range []os.FileMode{0o600, 0o400} {
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
		for name, load := range loaders() {
			c, err := load(env(map[string]string{"VQ_DATA_DIR": dir}))
			if err != nil || (name != "Local" && c.BlobBackend != "dir") {
				t.Fatalf("%o %s: %+v %v", mode, name, c, err)
			}
		}
	}
}

func TestEnvironmentOverridesFile(t *testing.T) {
	dir := t.TempDir()
	writeEnv(t, dir, "VQ_ADDR=127.0.0.1:7900\nVQ_BLOB_STORE=dir\nVQ_LOG_LEVEL=debug\n")
	c, err := FromEnv(env(map[string]string{"VQ_DATA_DIR": dir, "VQ_ADDR": "127.0.0.1:7999", "VQ_BLOB_STORE": ""}))
	if err != nil {
		t.Fatal(err)
	}
	if c.Addr != "127.0.0.1:7999" || c.BlobBackend != "dir" || c.LogLevel != "debug" || c.LogFormat != "text" {
		t.Fatalf("%+v", c)
	}
	for k, want := range map[string]string{
		"VQ_ADDR": SourceEnvironment, "VQ_BLOB_STORE": SourceFile, "VQ_LOG_FORMAT": SourceDefault, "VQ_DATA_DIR": SourceEnvironment,
	} {
		if got := c.Source(k); got != want {
			t.Fatalf("%s from %q, want %q", k, got, want)
		}
	}
	// Local reads the file too: the CLI finds a server on the file's port.
	l, err := Local(env(map[string]string{"VQ_DATA_DIR": dir}))
	if err != nil || l.Addr != "127.0.0.1:7900" {
		t.Fatalf("%+v %v", l, err)
	}
	// A bad server-only setting in the file still stops only serve.
	writeEnv(t, dir, "VQ_LOG_LEVEL=loud\n")
	if _, err := Local(env(map[string]string{"VQ_DATA_DIR": dir})); err != nil {
		t.Fatal(err)
	}
	if _, err := FromEnv(env(map[string]string{"VQ_DATA_DIR": dir})); err == nil || !strings.Contains(err.Error(), "VQ_LOG_LEVEL") {
		t.Fatalf("%v", err)
	}
}

func TestEnvFileS3EndpointRefusesUserinfo(t *testing.T) {
	dir := t.TempDir()
	writeEnv(t, dir, "VQ_BLOB_STORE=s3\nVQ_S3_ENDPOINT=user:hunter2pass@s3.example.com\nVQ_S3_BUCKET=b\nVQ_S3_ACCESS_KEY_ID=k\nVQ_S3_SECRET_ACCESS_KEY=s\n")
	_, err := FromEnv(env(map[string]string{"VQ_DATA_DIR": dir}))
	if err == nil || !strings.Contains(err.Error(), "can't hold a user name or password") || strings.Contains(err.Error(), "hunter2pass") {
		t.Fatalf("%v", err)
	}
}
