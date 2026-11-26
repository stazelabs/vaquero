package core

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/stazelabs/vaquero/internal/units"
)

// Origin is where a change came from, as the caller reported it: the
// surface it arrived on (web, cli, api, mcp), the vaquero client and its
// version, the harness and its version, the model, and the run. None of it
// is identity or verified; the actor is who made the change. emit stores it
// with each event as meta.
type Origin struct {
	Surface, Client, Harness, Model, Run string
}

// MaxOriginField is the most characters meta keeps of each value.
const MaxOriginField = 80

// Surfaces an Origin can name.
const (
	SurfaceWeb = "web"
	SurfaceCLI = "cli"
	SurfaceAPI = "api"
	SurfaceMCP = "mcp"
)

type originKey struct{}

// WithOrigin is ctx carrying o, for every event written under it.
func WithOrigin(ctx context.Context, o Origin) context.Context {
	return context.WithValue(ctx, originKey{}, o)
}

// OriginFrom is the Origin on ctx, or the zero Origin.
func OriginFrom(ctx context.Context) Origin {
	o, _ := ctx.Value(originKey{}).(Origin)
	return o
}

// meta is o as an event's meta: a JSON object of the fields that aren't
// empty, each trimmed and cut to MaxOriginField characters; {} when none.
func (o Origin) meta() string {
	m := map[string]string{}
	for k, v := range map[string]string{"surface": o.Surface, "client": o.Client, "harness": o.Harness, "model": o.Model, "run": o.Run} {
		if v = units.Clip(strings.TrimSpace(v), MaxOriginField); v != "" {
			m[k] = v
		}
	}
	// A map of strings always marshals (invalid UTF-8 becomes U+FFFD).
	b, _ := json.Marshal(m)
	return string(b)
}
