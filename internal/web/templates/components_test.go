package templates

import (
	"bytes"
	"context"
	"regexp"
	"testing"
)

// VAQ-109: a select beside a text field is as tall as it, so both carry
// the same fixed height (browsers size a select's box their own way).
func TestSelectMatchesTextInputHeight(t *testing.T) {
	var in, sel bytes.Buffer
	if err := TextInput(InputProps{Name: "to"}).Render(context.Background(), &in); err != nil {
		t.Fatal(err)
	}
	if err := Select(SelectProps{Name: "kind", Options: []Option{{"a", "A"}}}).Render(context.Background(), &sel); err != nil {
		t.Fatal(err)
	}
	height := regexp.MustCompile(`\bh-[0-9.]+\b`)
	hi, hs := height.FindString(in.String()), height.FindString(sel.String())
	if hi == "" || hi != hs {
		t.Fatalf("input height %q, select height %q", hi, hs)
	}
}
