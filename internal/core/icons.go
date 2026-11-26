package core

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/fnv"
	"net/http"
	"slices"
	"strings"
	"unicode"

	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/timefmt"
)

// MaxIcon is the largest project icon image, in bytes.
const MaxIcon = 256 << 10

// Icon kinds: a project shows its monogram unless it chose a glyph or an
// uploaded image.
const (
	IconKindMonogram = "monogram"
	IconKindGlyph    = "glyph"
	IconKindImage    = "image"
)

// iconTypes are the image types an icon may be, as sniffed from its bytes.
var iconTypes = []string{"image/png", "image/jpeg", "image/webp"}

// ProjectGlyphs is the curated set of glyphs a project icon may use, by
// name. Each is drawn through internal/web/icons as "glyph-" + name.
var ProjectGlyphs = []string{
	"folder", "rocket", "smartphone", "globe", "server", "database", "book", "wrench",
	"code", "terminal", "cloud", "cpu", "package", "layers", "palette", "camera",
	"music", "gamepad", "heart", "star", "sun", "leaf", "flame", "zap",
	"shield", "bell", "mail", "map", "compass", "truck", "plane", "briefcase",
	"graduation-cap", "flask", "bug", "puzzle", "lightbulb", "hammer", "chart", "users",
}

// IconImage is an uploaded icon image. Type is what the client claimed; it
// is never trusted: SetProjectIcon sniffs Data instead.
type IconImage struct {
	Type string
	Data []byte
}

// Monogram is a project's two upper-case characters: the first letter or
// digit of each of the first two words of name that have one, else the
// first two characters of key.
func Monogram(name, key string) string {
	var initials []rune
	for w := range strings.FieldsSeq(name) {
		if i := strings.IndexFunc(w, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }); i >= 0 {
			initials = append(initials, []rune(w[i:])[0])
		}
		if len(initials) == 2 {
			return strings.ToUpper(string(initials))
		}
	}
	k := []rune(key)
	return strings.ToUpper(string(k[:min(2, len(k))]))
}

// TileColor is the monogram tile's colour pair, 0 to 7: a stable FNV-1a
// hash of the project's id, so a key change never recolours a project.
func TileColor(projectID string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(projectID))
	return int(h.Sum32() % 8)
}

// storedIcon is an icon image ready to store: its sniffed type, base64
// text and etag.
type storedIcon struct{ typ, data, etag string }

// checkIconImage refuses an image that isn't a PNG, JPEG or WebP of at most
// MaxIcon bytes, by its bytes alone, against field: a project icon's and an
// avatar's check alike (VAQ-149).
func checkIconImage(f fields, field string, img *IconImage) *storedIcon {
	if len(img.Data) > MaxIcon {
		f.add(field, "Use an image of 256 KB or less.")
		return nil
	}
	typ := http.DetectContentType(img.Data)
	if !slices.Contains(iconTypes, typ) {
		f.add(field, "Use a PNG, JPEG or WebP image.")
		return nil
	}
	sum := sha256.Sum256(img.Data)
	return &storedIcon{typ: typ, data: base64.StdEncoding.EncodeToString(img.Data), etag: hex.EncodeToString(sum[:16])}
}

// iconValue is how an event names an icon: its kind, and its glyph
// (applyIcon adds an image's etag).
func iconValue(kind, glyph string) map[string]string {
	if kind == IconKindGlyph {
		return map[string]string{"kind": kind, "glyph": glyph}
	}
	return map[string]string{"kind": kind}
}

// applyIcon checks kind and glyph (and img, when given) for project p
// inside q's transaction and writes them. It reports the change for the
// event, nil when nothing changed. Refusals go into f, and then nothing is
// written.
func applyIcon(ctx context.Context, q *gen.Queries, f fields, p *gen.Project, kind, glyph string, img *IconImage, now string) (*Change, error) {
	var stored *storedIcon
	switch kind {
	case IconKindMonogram, IconKindImage:
		glyph = ""
	case IconKindGlyph:
		if !slices.Contains(ProjectGlyphs, glyph) {
			f.add("icon", "Choose a glyph from the list.")
		}
	default:
		f.add("icon", "Choose a monogram, a glyph or an image.")
	}
	if kind == IconKindImage && img != nil {
		stored = checkIconImage(f, "icon", img)
	}
	var oldEtag string
	if kind == IconKindImage && len(f) == 0 {
		var err error
		oldEtag, err = q.GetProjectIconEtag(ctx, p.ID)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			if stored == nil {
				f.add("icon", "Choose an image to upload.")
			}
		case err != nil:
			return nil, fmt.Errorf("get icon: %w", err)
		}
	}
	if len(f) > 0 {
		return nil, nil
	}
	newImage := stored != nil && stored.etag != oldEtag
	if kind == p.IconKind && glyph == p.IconGlyph && !newImage {
		return nil, nil
	}
	if newImage {
		if err := q.SaveProjectIcon(ctx, gen.SaveProjectIconParams{ProjectID: p.ID, Type: stored.typ, Data: stored.data, Etag: stored.etag, UpdatedAt: now}); err != nil {
			return nil, fmt.Errorf("save icon: %w", err)
		}
	}
	if kind != p.IconKind || glyph != p.IconGlyph {
		if err := q.SetProjectIconKind(ctx, gen.SetProjectIconKindParams{IconKind: kind, IconGlyph: glyph, UpdatedAt: now, ID: p.ID}); err != nil {
			return nil, fmt.Errorf("set icon: %w", err)
		}
	}
	from, to := iconValue(p.IconKind, p.IconGlyph), iconValue(kind, glyph)
	if kind == IconKindImage {
		// Each image side names its etag, so replacing one image with
		// another shows as a change though the kind stays "image".
		if p.IconKind == IconKindImage && oldEtag != "" {
			from["etag"] = oldEtag
		}
		to["etag"] = oldEtag
		if stored != nil {
			to["etag"] = stored.etag
		}
	}
	ch := &Change{From: from, To: to}
	p.IconKind, p.IconGlyph, p.UpdatedAt = kind, glyph, now
	return ch, nil
}

// SetProjectIcon gives a project its monogram, a glyph from ProjectGlyphs,
// or an image. For "image", a new image (PNG, JPEG or WebP, at most
// MaxIcon bytes, its type sniffed from the bytes) replaces the stored one;
// without one the stored image is used again. It writes one
// project.updated event with changes.icon, or nothing when nothing
// changes. The key never changes here.
func (c *Core) SetProjectIcon(ctx context.Context, by, projectID string, kind, glyph string, image *IconImage) (gen.Project, error) {
	var p gen.Project
	err := c.tx(ctx, func(q *gen.Queries) error {
		var err error
		if p, err = q.GetProject(ctx, projectID); err != nil {
			return notFound(err, "get project")
		}
		f := fields{}
		ch, err := applyIcon(ctx, q, f, &p, kind, glyph, image, timefmt.Format(c.Now()))
		if err != nil {
			return err
		}
		if err := f.err(); err != nil {
			return err
		}
		if ch == nil {
			return nil
		}
		return c.emit(ctx, q, by, EvProjectUpdated, p.ID, "", map[string]any{"changes": map[string]Change{"icon": *ch}})
	})
	return p, err
}

// RemoveProjectIconImage deletes a project's stored icon image (VAQ-146).
// An image in use gives way to the monogram; a monogram or glyph in use
// stays. It writes one project.updated event with changes.icon_image (the
// removed image's etag to null), plus changes.icon when the icon in use
// changed; with no image stored it writes nothing.
func (c *Core) RemoveProjectIconImage(ctx context.Context, by, projectID string) (gen.Project, error) {
	var p gen.Project
	err := c.tx(ctx, func(q *gen.Queries) error {
		var err error
		if p, err = q.GetProject(ctx, projectID); err != nil {
			return notFound(err, "get project")
		}
		etag, err := q.GetProjectIconEtag(ctx, p.ID)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("get icon: %w", err)
		}
		if err := q.DeleteProjectIcon(ctx, p.ID); err != nil {
			return fmt.Errorf("delete icon: %w", err)
		}
		now := timefmt.Format(c.Now())
		changes := map[string]Change{"icon_image": {From: etag, To: nil}}
		if p.IconKind == IconKindImage {
			from := iconValue(IconKindImage, "")
			from["etag"] = etag
			changes["icon"] = Change{From: from, To: iconValue(IconKindMonogram, "")}
			p.IconKind, p.IconGlyph = IconKindMonogram, ""
		}
		if err := q.SetProjectIconKind(ctx, gen.SetProjectIconKindParams{IconKind: p.IconKind, IconGlyph: p.IconGlyph, UpdatedAt: now, ID: p.ID}); err != nil {
			return fmt.Errorf("set icon: %w", err)
		}
		p.UpdatedAt = now
		return c.emit(ctx, q, by, EvProjectUpdated, p.ID, "", map[string]any{"changes": changes})
	})
	return p, err
}
