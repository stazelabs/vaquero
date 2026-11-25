package handlers

import (
	"errors"
	"net/http"
	"time"

	"github.com/stazelabs/vaquero/internal/api"
	"github.com/stazelabs/vaquero/internal/archive"
	"github.com/stazelabs/vaquero/internal/core"
	appdb "github.com/stazelabs/vaquero/internal/db"
	"github.com/stazelabs/vaquero/internal/timefmt"
	"github.com/stazelabs/vaquero/internal/version"
	"github.com/stazelabs/vaquero/internal/web/middleware"
)

// exportStart is the response writer an export streams into: the archive's
// headers and 200 are sent with its first byte, so a failure before then
// can still answer with an API error.
type exportStart struct {
	w        http.ResponseWriter
	filename string
	started  bool
}

func (s *exportStart) Write(p []byte) (int, error) {
	if !s.started {
		s.started = true
		h := s.w.Header()
		h.Set("Content-Type", "application/gzip")
		h.Set("Content-Disposition", `attachment; filename="`+s.filename+`"`)
		s.w.WriteHeader(http.StatusOK)
	}
	return s.w.Write(p)
}

// APIExport is GET /api/v1/export: the whole instance as a .tar.gz, for
// the owner only. It reads through the reader pool and the blob store, never
// the writer. A failure once the archive has started aborts the connection,
// so the client sees a broken download rather than a short archive.
func APIExport(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !core.IsOwner(apiActor(r)) {
			api.WriteFailure(w, api.Fail(http.StatusForbidden, "forbidden", "Only the owner can export."))
			return
		}
		schema, err := appdb.Version(d.RDB)
		if err != nil {
			apiFail(w, r, d, api.Internal(), err)
			return
		}
		// A big export can outlast the server's write timeout.
		_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})
		now := d.Now()
		out := &exportStart{w: w, filename: "vaquero-" + timefmt.FileStamp(now) + ".tar.gz"}
		err = archive.Write(r.Context(), out, d.RDB, d.Blobs, archive.NewHeader(schema, now, version.Version))
		if err == nil {
			return
		}
		d.Logger.ErrorContext(r.Context(), "export", "err", err, "started", out.started,
			"request_id", middleware.GetRequestID(r.Context()))
		if out.started {
			panic(http.ErrAbortHandler)
		}
		if missing, ok := errors.AsType[*archive.MissingFileError](err); ok {
			api.WriteFailure(w, api.Fail(http.StatusInternalServerError, "missing_file", missing.Error()))
			return
		}
		api.WriteFailure(w, api.Internal())
	}
}
