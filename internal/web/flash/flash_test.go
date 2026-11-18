package flash_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stazelabs/vaquero/internal/web/flash"
)

func TestFlashRoundTrip(t *testing.T) {
	w := httptest.NewRecorder()
	flash.Put(w, flash.Success, "Ticket VAQ-4 created.")
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != "vq_flash" || !cookies[0].HttpOnly {
		t.Fatalf("%+v", cookies)
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(cookies[0])
	w2 := httptest.NewRecorder()
	f, ok := flash.Pop(w2, r)
	if !ok || f.Kind != flash.Success || f.Message != "Ticket VAQ-4 created." {
		t.Fatalf("%+v %v", f, ok)
	}
	if c := w2.Result().Cookies(); len(c) != 1 || c[0].MaxAge >= 0 {
		t.Fatalf("cookie not expired: %+v", c)
	}
}

func TestPopWithoutCookie(t *testing.T) {
	w := httptest.NewRecorder()
	if _, ok := flash.Pop(w, httptest.NewRequest("GET", "/", nil)); ok {
		t.Fatal("popped from nothing")
	}
	if len(w.Result().Cookies()) != 0 {
		t.Fatal("set a cookie for nothing")
	}
}

func TestMalformedFlashIgnored(t *testing.T) {
	for _, v := range []string{"garbage!!", "e30", "eyJLaW5kIjoic3VjY2VzcyJ9"} { // invalid base64, {}, kind without message
		r := httptest.NewRequest("GET", "/", nil)
		r.AddCookie(&http.Cookie{Name: "vq_flash", Value: v})
		w := httptest.NewRecorder()
		if f, ok := flash.Pop(w, r); ok {
			t.Errorf("%q popped %+v", v, f)
		}
		if c := w.Result().Cookies(); len(c) != 1 || c[0].MaxAge >= 0 {
			t.Errorf("%q: cookie not expired", v)
		}
	}
}
