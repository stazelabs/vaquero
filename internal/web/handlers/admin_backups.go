package handlers

import (
	"errors"
	"net/http"
	"net/url"

	"github.com/go-chi/chi/v5"

	appdb "github.com/stazelabs/vaquero/internal/db"
	"github.com/stazelabs/vaquero/internal/timefmt"
	"github.com/stazelabs/vaquero/internal/units"
	"github.com/stazelabs/vaquero/internal/web/flash"
	"github.com/stazelabs/vaquero/internal/web/templates"
)

// The admin backups page (VAQ-150): the snapshots in Deps.BackupDir, a
// snapshot taken now, and a delete. Neither is a tracker change, so
// neither writes an event; a restore stays offline (vaquero backup
// restore), and each row shows its command.

const backupsPath = "/admin/backups"

// AdminBackups is GET /admin/backups.
func AdminBackups(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		all, err := appdb.ListBackups(d.BackupDir)
		if err != nil {
			serverError(w, r, d, err)
			return
		}
		v := templates.BackupsView{Dir: d.BackupDir, Brew: d.BrewService != nil && d.BrewService()}
		if free, err := appdb.FreeBytes(d.BackupDir); err == nil {
			v.Free = units.SizeWords(free)
		} else {
			d.Logger.WarnContext(r.Context(), "backups free space", "err", err)
		}
		for _, b := range all {
			row := templates.BackupRow{Name: b.Name, Kind: backupKindLabel(b.Kind), Size: units.SizeWords(b.Size),
				Restore: "vaquero backup restore " + b.Name}
			if !b.Taken.IsZero() {
				row.Date = timefmt.DisplayUTC(b.Taken)
			}
			if s, ok := appdb.BackupSchema(d.BackupDir, b.Name); ok {
				row.Schema = s
			}
			v.Rows = append(v.Rows, row)
		}
		render(w, r, d, http.StatusOK, templates.BackupsPage(v))
	}
}

// backupKindLabel says why a snapshot was taken.
func backupKindLabel(k appdb.Kind) string {
	switch k {
	case appdb.KindManual:
		return "Taken from this page"
	case appdb.KindPreMigrate:
		return "Before an upgrade"
	case appdb.KindPreDelete:
		return "Before a project delete"
	case appdb.KindPreRestore:
		return "Before a restore"
	case appdb.KindPreImport:
		return "Before an import"
	case appdb.KindPreCompact:
		return "Before a compaction"
	case appdb.KindPreMove:
		return "Before an attachments move"
	}
	return "From an older release"
}

// AdminBackupSnapshot is POST /admin/backups/snapshot: a manual snapshot
// while the server runs, then pruning, as for every kind.
func AdminBackupSnapshot(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		info, err := appdb.SnapshotNext(r.Context(), d.RDB, d.BackupDir, appdb.KindManual, d.Now())
		switch {
		case errors.Is(err, appdb.ErrLowSpace):
			redirectWithFlash(w, r, flash.Error, "Not enough free disk space to save a snapshot.", backupsPath)
			return
		case err != nil:
			d.Logger.ErrorContext(r.Context(), "manual snapshot", "err", err)
			redirectWithFlash(w, r, flash.Error, "Couldn't save a snapshot in "+d.BackupDir+".", backupsPath)
			return
		}
		if _, err := appdb.PruneBackups(d.BackupDir, appdb.BackupKeep); err != nil {
			d.Logger.WarnContext(r.Context(), "prune backups", "err", err)
		}
		redirectWithFlash(w, r, flash.Success, "Snapshot saved: "+info.Name+".", backupsPath)
	}
}

// AdminBackupDelete is POST /admin/backups/{name}/delete, behind the
// confirm dialog. It deletes only a snapshot the list shows
// (appdb.DeleteBackup: a snapshot's name, a regular file in the backups
// directory); any other name is a 404 and touches nothing.
func AdminBackupDelete(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := chi.URLParam(r, "name")
		if r.URL.RawPath != "" {
			n, err := url.PathUnescape(name)
			if err != nil {
				NotFound(d)(w, r)
				return
			}
			name = n
		}
		err := appdb.DeleteBackup(d.BackupDir, name)
		switch {
		case errors.Is(err, appdb.ErrBadBackupName), errors.Is(err, appdb.ErrBackupNotFound):
			NotFound(d)(w, r)
		case err != nil:
			serverError(w, r, d, err)
		default:
			redirectWithFlash(w, r, flash.Success, "Snapshot deleted: "+name+".", backupsPath)
		}
	}
}
