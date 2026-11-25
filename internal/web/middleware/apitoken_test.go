package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/dbtest"
	"github.com/stazelabs/vaquero/internal/db/gen"
)

type noTouch struct{}

func (noTouch) TouchToken(context.Context, gen.TouchTokenParams) error { return nil }

// VAQ-64: an agent that already exists is found through the reader; the
// writer is used only to make it. Names stay case-sensitive, so "owner"
// and "Owner" are agents, never the human Owner.
func TestAgentHeaderLooksUpThroughReader(t *testing.T) {
	db := dbtest.New(t)
	now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	c := &core.Core{DB: db.W, Now: func() time.Time { return now }}
	owner, err := c.EnsureOwner(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	raw, _, err := c.CreateToken(context.Background(), owner.ID, owner.ID, "t")
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	auth := APIAuth{R: db.RQ, Touch: noTouch{}, Now: c.Now, Logger: quiet, Local: true, OwnerID: owner.ID,
		Agent: func(ctx context.Context, by, name string) (gen.Actor, error) {
			calls++
			return c.EnsureAgent(ctx, by, name)
		}}
	var got gen.Actor
	h := APIToken(auth)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { got, _ = APIActor(r.Context()) }))
	do := func(name string) {
		req := httptest.NewRequest("GET", "/api/v1/me", nil)
		req.Header.Set("Authorization", "Bearer "+raw)
		req.Header.Set(AgentHeader, name)
		h.ServeHTTP(httptest.NewRecorder(), req)
	}
	do("builder")
	first := got
	do("builder")
	do(" builder ")
	if calls != 1 || got.ID != first.ID || got.Kind != "agent" {
		t.Fatalf("writer path used %d times; actor %+v", calls, got)
	}
	do("owner")
	if got.ID == owner.ID || got.Kind != "agent" || got.Name != "owner" {
		t.Fatalf("lower-case owner merged: %+v", got)
	}
}
