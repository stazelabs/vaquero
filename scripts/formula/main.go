// Command formula writes the Homebrew formula for a release from
// packaging/homebrew/vaquero.rb.tmpl and the release's checksums.txt: the
// version, and the URL and SHA-256 of the four archives (macOS and Linux,
// amd64 and arm64). The release workflow runs it after goreleaser and puts
// the result in Formula/vaquero.rb of stazelabs/homebrew-vaquero. Run from
// the repository root:
//
//	go run ./scripts/formula -version 0.1.3 -checksums dist/checksums.txt
//
// It replaces goreleaser's deprecated `brews`, whose successor writes casks,
// which have no service block for brew services.
package main

import (
	"bufio"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"text/template"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "formula:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("formula", flag.ContinueOnError)
	version := fs.String("version", "", "the release, like 0.1.3 (a leading v is dropped)")
	sums := fs.String("checksums", "dist/checksums.txt", "goreleaser's checksums.txt for the release")
	tmpl := fs.String("template", "packaging/homebrew/vaquero.rb.tmpl", "the formula template")
	out := fs.String("o", "", "where to write the formula (standard output when empty)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *version == "" {
		return errors.New("-version is required")
	}
	t, err := os.ReadFile(*tmpl)
	if err != nil {
		return err
	}
	f, err := os.Open(*sums)
	if err != nil {
		return err
	}
	defer f.Close()
	b, err := render(string(t), *version, f)
	if err != nil {
		return err
	}
	if *out == "" {
		_, err = os.Stdout.Write(b)
		return err
	}
	return os.WriteFile(*out, b, 0o644)
}

// archive is one platform's download in the formula.
type archive struct {
	URL    string
	SHA256 string
}

// data is what the template sees.
type data struct {
	Version                  string
	DarwinAMD64, DarwinARM64 archive
	LinuxAMD64, LinuxARM64   archive
}

var (
	// A release number, or a snapshot's (0.1.3-SNAPSHOT-abc1234) for a
	// local check; the release workflow takes only vX.Y.Z tags.
	versionRE  = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$`)
	checksumRE = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// render fills the template for version from checksums (goreleaser's
// "<sha256>  <file>" lines). Every one of the four archives must be listed
// exactly once with a well-formed checksum; other files are ignored.
func render(tmpl, version string, checksums io.Reader) ([]byte, error) {
	version = strings.TrimPrefix(version, "v")
	if !versionRE.MatchString(version) {
		return nil, fmt.Errorf("version %q isn't a release number like 0.1.3", version)
	}
	sums := map[string]string{}
	sc := bufio.NewScanner(checksums)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		f := strings.Fields(line)
		if len(f) != 2 {
			return nil, fmt.Errorf("checksums line %d isn't \"<sha256>  <file>\": %q", n, line)
		}
		if _, dup := sums[f[1]]; dup {
			return nil, fmt.Errorf("checksums list %s twice", f[1])
		}
		sums[f[1]] = f[0]
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	get := func(goos, goarch string) (archive, error) {
		name := fmt.Sprintf("vaquero_%s_%s_%s.tar.gz", version, goos, goarch)
		sum, ok := sums[name]
		if !ok {
			return archive{}, fmt.Errorf("checksums don't list %s", name)
		}
		if !checksumRE.MatchString(sum) {
			return archive{}, fmt.Errorf("the checksum of %s isn't 64 lower-case hex digits: %q", name, sum)
		}
		return archive{
			URL:    fmt.Sprintf("https://github.com/stazelabs/vaquero/releases/download/v%s/%s", version, name),
			SHA256: sum,
		}, nil
	}
	d := data{Version: version}
	for _, p := range []struct {
		goos, goarch string
		into         *archive
	}{
		{"darwin", "amd64", &d.DarwinAMD64},
		{"darwin", "arm64", &d.DarwinARM64},
		{"linux", "amd64", &d.LinuxAMD64},
		{"linux", "arm64", &d.LinuxARM64},
	} {
		a, err := get(p.goos, p.goarch)
		if err != nil {
			return nil, err
		}
		*p.into = a
	}
	t, err := template.New("formula").Option("missingkey=error").Parse(tmpl)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, d); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
