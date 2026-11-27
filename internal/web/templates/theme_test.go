package templates

import (
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// themeBlocks reads input.css's @theme block (the light tokens) and the
// dark-mode override block, each as token name → value.
func themeBlocks(t *testing.T) (light, dark map[string]string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "static", "css", "input.css"))
	if err != nil {
		t.Fatal(err)
	}
	css := string(b)
	block := func(start string) string {
		i := strings.Index(css, start)
		if i < 0 {
			t.Fatalf("input.css has no %q", start)
		}
		depth, open := 0, strings.Index(css[i:], "{")+i
		for j := open; j < len(css); j++ {
			switch css[j] {
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					return css[open+1 : j]
				}
			}
		}
		t.Fatalf("%q isn't closed", start)
		return ""
	}
	decl := regexp.MustCompile(`(--[a-z0-9-]+)\s*:\s*([^;]+);`)
	read := func(s string) map[string]string {
		m := map[string]string{}
		for _, d := range decl.FindAllStringSubmatch(s, -1) {
			m[d[1]] = strings.TrimSpace(d[2])
		}
		return m
	}
	return read(block("@theme {")), read(block("@media (prefers-color-scheme: dark) {"))
}

type oklch struct{ L, C, H float64 }

var oklchValue = regexp.MustCompile(`^oklch\(\s*([0-9.]+)\s+([0-9.]+)\s+([0-9.]+)\s*\)$`)

func parseOKLCH(t *testing.T, name, v string) oklch {
	t.Helper()
	m := oklchValue.FindStringSubmatch(v)
	if m == nil {
		t.Fatalf("%s = %q, want oklch(L C H)", name, v)
	}
	f := func(s string) float64 { x, _ := strconv.ParseFloat(s, 64); return x }
	return oklch{f(m[1]), f(m[2]), f(m[3])}
}

// luminance is the WCAG relative luminance of an oklch colour, through
// OKLab to linear sRGB (clamped to the gamut).
func (c oklch) luminance() float64 {
	h := c.H * math.Pi / 180
	a, b := c.C*math.Cos(h), c.C*math.Sin(h)
	l := math.Pow(c.L+0.3963377774*a+0.2158037573*b, 3)
	m := math.Pow(c.L-0.1055613458*a-0.0638541728*b, 3)
	s := math.Pow(c.L-0.0894841775*a-1.2914855480*b, 3)
	clamp := func(x float64) float64 { return math.Max(0, math.Min(1, x)) }
	r := clamp(4.0767416621*l - 3.3077115913*m + 0.2309699292*s)
	g := clamp(-1.2684380046*l + 2.6097574011*m - 0.3413193965*s)
	bl := clamp(-0.0041960863*l - 0.7034186147*m + 1.7076147010*s)
	return 0.2126*r + 0.7152*g + 0.0722*bl
}

func contrast(x, y oklch) float64 {
	a, b := x.luminance()+0.05, y.luminance()+0.05
	return math.Max(a, b) / math.Min(a, b)
}

func TestContrastHelper(t *testing.T) {
	white, black := oklch{1, 0, 0}, oklch{0, 0, 0}
	if c := contrast(white, black); math.Abs(c-21) > 0.05 {
		t.Fatalf("white on black is %.2f:1, want 21:1", c)
	}
}

// VAQ-171: the type, height and radius scale is a set of tokens, so no
// template sets a size ad hoc; the palette is near-neutral and readable.
func TestThemeTokens(t *testing.T) {
	light, dark := themeBlocks(t)
	for name, want := range map[string]string{
		"--text-xs": "12px", "--text-xs--line-height": "16px",
		"--text-sm": "13px", "--text-sm--line-height": "18px",
		"--text-base": "15px", "--text-base--line-height": "22px",
		"--text-lg": "18px", "--text-lg--line-height": "24px",
		"--spacing-row": "28px", "--spacing-nav": "26px",
		"--spacing-chip": "24px", "--spacing-bar": "38px",
		"--radius-sm": "4px", "--radius-md": "6px",
	} {
		if got := light[name]; got != want {
			t.Errorf("@theme %s = %q, want %q", name, got, want)
		}
	}
	for scheme, tokens := range map[string]map[string]string{"light": light, "dark": dark} {
		col := func(n string) oklch { return parseOKLCH(t, scheme+" "+n, tokens["--color-"+n]) }
		for _, n := range []string{"paper", "paper-alt", "line"} {
			if c := col(n); c.C > 0.006 {
				t.Errorf("%s %s has chroma %v, want at most 0.006", scheme, n, c.C)
			}
		}
		for _, p := range [][2]string{{"muted", "paper"}, {"ink", "paper-alt"}, {"ink", "paper"}} {
			if r := contrast(col(p[0]), col(p[1])); r < 4.5 {
				t.Errorf("%s %s on %s is %.2f:1, want at least 4.5:1", scheme, p[0], p[1], r)
			}
		}
	}
}

func TestNoAdHocSizes(t *testing.T) {
	files, _ := filepath.Glob("*.templ")
	goFiles, _ := filepath.Glob("*.go")
	files = append(files, goFiles...)
	adhoc := regexp.MustCompile(`text-\[1[013]px\]`)
	for _, f := range files {
		if strings.HasSuffix(f, "_templ.go") || strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, _ := os.ReadFile(f)
		for _, m := range adhoc.FindAllString(string(b), -1) {
			t.Errorf("%s uses %s; use the text-xs, text-sm, text-base or text-lg tokens", f, m)
		}
	}
}
