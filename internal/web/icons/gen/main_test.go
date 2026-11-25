package main

import (
	"strings"
	"testing"
)

func TestUIInnerKeepsWhatItParses(t *testing.T) {
	svg := "<!-- licence -->\n<svg xmlns=\"http://www.w3.org/2000/svg\" width=\"24\">\n  <path d=\"M1 1L2 2\" />\n  <circle cx=\"1\" cy=\"2\" r=\"3\" fill=\"currentColor\" />\n</svg>"
	got, err := uiInner("x", svg)
	if err != nil {
		t.Fatal(err)
	}
	if want := `<path d="M1 1L2 2"></path><circle cx="1" cy="2" r="3" fill="currentColor"></circle>`; got != want {
		t.Fatalf("got %s", got)
	}
}

// An element or attribute the generator can't read stops it, rather than
// the icon losing a stroke without anyone noticing.
func TestUIInnerRefusesWhatItCannotParse(t *testing.T) {
	for name, svg := range map[string]string{
		"single quotes":     `<svg><path d='M1 1'/></svg>`,
		"unknown element":   `<svg><path d="M1 1"/><text>hi</text></svg>`,
		"script":            `<svg><script>alert(1)</script></svg>`,
		"uppercase element": `<svg><PATH d="M1 1"/></svg>`,
		"bad attribute":     `<svg><path d="M1 1" onclick="x"/></svg>`,
		"bad path data":     `<svg><path d="url(javascript:1)"/></svg>`,
		"empty":             `<svg></svg>`,
	} {
		if _, err := uiInner("x", svg); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := uiInner("x", `<svg><path d='M1 1'/></svg>`); err == nil || !strings.Contains(err.Error(), "can't parse") {
		t.Errorf("single quotes: %v", err)
	}
}
