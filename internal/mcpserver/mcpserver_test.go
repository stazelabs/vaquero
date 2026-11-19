package mcpserver

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/web/middleware"
)

// A tool whose API call panics answers the plain tool error, and the panic
// is logged with the tool's name.
func TestMCPToolPanicRecovered(t *testing.T) {
	var logs bytes.Buffer
	boom := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") })
	h := Handler(boom, slog.New(slog.NewTextHandler(&logs, nil)))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTP(w, r.WithContext(middleware.WithAPIActor(r.Context(), gen.Actor{ID: "a", Name: "Owner", Kind: "human"})))
	}))
	defer srv.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "t", Version: "1"}, nil)
	cs, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{Endpoint: srv.URL, DisableStandaloneSSE: true, MaxRetries: -1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: "list_projects", Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || res.Content[0].(*mcp.TextContent).Text != "Something went wrong." {
		t.Fatalf("%+v", res)
	}
	if !strings.Contains(logs.String(), "mcp tool panic") || !strings.Contains(logs.String(), "list_projects") {
		t.Fatalf("not logged: %s", logs.String())
	}
}
