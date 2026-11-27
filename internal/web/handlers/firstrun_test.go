package handlers_test

import (
	"testing"

	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/web/webtest"
)

const connectLine = "claude mcp add vaquero -- vaquero mcp --agent NAME"

// VAQ-162: an empty instance and an empty project say what to do next and
// show the one line that connects an agent.
func TestEmptyStatesNameTheNextStep(t *testing.T) {
	e := webtest.New(t)
	c := e.Client()
	home := c.Get("/").Body
	contains(t, home, "No projects yet.", connectLine, `href="/projects/new"`, `href="/help/mcp"`, "<pre")
	p := project(t, e)
	lacks(t, c.Get("/").Body, connectLine)
	focus := c.Get("/p/VAQ").Body
	contains(t, focus, "No tickets yet.", connectLine, `href="/p/VAQ/new"`, `href="/help/mcp"`, "Nothing ready. Triage the backlog.")
	// A filter that hides everything isn't an empty project.
	lacks(t, c.Get("/p/VAQ?tag=nothing").Body, connectLine)
	ticket(t, e, p, core.TicketInput{Title: "first"})
	lacks(t, c.Get("/p/VAQ").Body, connectLine, "No tickets yet.")
	lacks(t, c.Get("/p/VAQ/focus/fragment").Body, connectLine)
}

// VAQ-163: a project key can be changed, in settings.
func TestNewProjectFormSaysKeyCanBeChanged(t *testing.T) {
	body := webtest.New(t).Client().Get("/projects/new").Body
	lacks(t, body, "can't be changed later")
	contains(t, body, "can be changed in the project's settings")
}
