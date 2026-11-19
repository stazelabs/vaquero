package handlers

import (
	"database/sql"
	"net/http"
	"net/url"
	"strings"

	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/web/templates"
)

// mcpPage is how many calls /admin/mcp shows at once.
const mcpPage = 100

// AdminMCP is GET /admin/mcp?agent=&tool=&before=: the MCP activity log,
// newest first, filtered by agent and tool, paged back by call.
func AdminMCP(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		v := templates.MCPView{Agent: strings.TrimSpace(q.Get("agent")), Tool: strings.TrimSpace(q.Get("tool")), Now: d.Now()}
		null := func(s string) any {
			if s == "" {
				return nil
			}
			return s
		}
		rows, err := d.R.ListMCPCalls(r.Context(), gen.ListMCPCallsParams{
			Actor: null(v.Agent), Tool: null(v.Tool), Before: null(q.Get("before")), Lim: mcpPage + 1,
		})
		if err != nil {
			serverError(w, r, d, err)
			return
		}
		if len(rows) > mcpPage {
			rows = rows[:mcpPage]
			older := url.Values{"before": {rows[len(rows)-1].ID}}
			if v.Agent != "" {
				older.Set("agent", v.Agent)
			}
			if v.Tool != "" {
				older.Set("tool", v.Tool)
			}
			v.Older = "/admin/mcp?" + older.Encode()
		}
		v.Calls = rows
		if v.Agents, err = d.R.ListMCPCallActors(r.Context()); err != nil && err != sql.ErrNoRows {
			serverError(w, r, d, err)
			return
		}
		if v.Tools, err = d.R.ListMCPCallTools(r.Context()); err != nil && err != sql.ErrNoRows {
			serverError(w, r, d, err)
			return
		}
		render(w, r, d, http.StatusOK, templates.MCPPage(v))
	}
}
