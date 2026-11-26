//go:build e2e

package e2e

import (
	"fmt"
	"strings"
	"testing"

	appdb "github.com/stazelabs/vaquero/internal/db"
	"github.com/stazelabs/vaquero/internal/web/webtest"
)

// VAQ-150: the backups page at phone and desktop widths, light and dark.
// Take a snapshot now shows it with its restore command; Delete asks in
// the confirm dialog, then removes it; nothing scrolls sideways.
func TestE2EBackupsPage(t *testing.T) {
	for _, w := range []int64{375, 1280} {
		for _, scheme := range []string{"light", "dark"} {
			t.Run(fmt.Sprintf("%d-%s", w, scheme), func(t *testing.T) {
				env := webtest.New(t)
				name := appdb.BackupNameKind(appdb.KindManual, webtest.Start)
				p := open(t, env, w, 900)
				p.scheme(scheme)
				p.goTo("/admin/backups")
				p.reloaded(func() { p.click(`form[action="/admin/backups/snapshot"] button`) },
					`!!document.querySelector('[data-backup="`+name+`"]')`)
				if got := eval[string](p, `document.querySelector('[data-backup="`+name+`"] code').textContent`); got != "vaquero backup restore "+name {
					t.Fatalf("restore command %q", got)
				}
				toast := eval[string](p, `(document.getElementById("vq-toast")||{}).textContent || ""`)
				if want := "Snapshot saved: " + name + "."; !strings.Contains(toast, want) {
					t.Fatalf("toast %q", toast)
				}
				bg := eval[string](p, `getComputedStyle(document.body).backgroundColor`)
				other := map[string]string{"light": "dark", "dark": "light"}[scheme]
				p.scheme(other)
				if again := eval[string](p, `getComputedStyle(document.body).backgroundColor`); again == bg {
					t.Fatalf("the page looks the same in %s and %s: %q", scheme, other, bg)
				}
				p.scheme(scheme)
				if sw := eval[int64](p, `document.documentElement.scrollWidth`); sw > w {
					t.Fatalf("the backups page is %d px wide at %d px", sw, w)
				}
				shot(t, p, "body", fmt.Sprintf("backups-%d-%s", w, scheme))
				p.click(`[data-backup="` + name + `"] form[data-confirm] button[type="submit"]`)
				p.until(`document.getElementById("vq-confirm").open`)
				if msg := eval[string](p, `document.querySelector("[data-confirm-message]").textContent`); msg != "Delete "+name+"? It can't be undone." {
					t.Fatalf("dialog asks %q", msg)
				}
				p.reloaded(func() { p.click("[data-confirm-ok]") }, `!document.querySelector('[data-backup="`+name+`"]')`)
				if list, _ := appdb.ListBackups(env.Deps.BackupDir); len(list) != 0 {
					t.Fatalf("%+v", list)
				}
				// The overview's system details and Storage section fit too.
				p.goTo("/admin")
				if !eval[bool](p, visible(`#storage`)) || !eval[bool](p, visible(`#system`)) {
					t.Fatal("no system details or storage section")
				}
				if sw := eval[int64](p, `document.documentElement.scrollWidth`); sw > w {
					t.Fatalf("the admin page is %d px wide at %d px", sw, w)
				}
				shot(t, p, "body", fmt.Sprintf("admin-%d-%s", w, scheme))
			})
		}
	}
}
