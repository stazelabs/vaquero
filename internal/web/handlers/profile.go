package handlers

import (
	"database/sql"
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/web/flash"
	"github.com/stazelabs/vaquero/internal/web/middleware"
	"github.com/stazelabs/vaquero/internal/web/templates"
)

// The owner's profile (VAQ-149): the page at /profile, linked from the
// header, holds the display name and the avatar. It isn't under /admin: in
// hosted mode each person has a profile of their own.

// profilePage renders /profile with the name field's value and refusal.
func profilePage(w http.ResponseWriter, r *http.Request, d Deps, status int, name, msg string) {
	render(w, r, d, status, templates.ProfilePage(templates.ProfileView{Owner: templates.OwnerView(r.Context()), DisplayName: name, Error: msg}))
}

// Profile is GET /profile.
func Profile(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a, err := d.R.GetActor(r.Context(), d.Actor.ID)
		if err != nil {
			serverError(w, r, d, err)
			return
		}
		profilePage(w, r, d, http.StatusOK, a.DisplayName, "")
	}
}

// ProfileSave is POST /profile, the name form: "" shows Owner again. A
// refusal re-renders the page with 422 and what was typed.
func ProfileSave(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PostFormValue("display_name")
		_, err := d.Core.UpdateProfile(r.Context(), d.Actor.ID, &name)
		if inv, ok := errors.AsType[*core.Invalid](err); ok {
			templates.LoadEventSeq(r.Context())
			msg := inv.Fields["display_name"]
			if msg == "" {
				msg = inv.Error()
			}
			profilePage(w, r, d, http.StatusUnprocessableEntity, name, msg)
			return
		}
		if err != nil {
			serverError(w, r, d, err)
			return
		}
		redirectWithFlash(w, r, flash.Success, "Profile saved.", "/profile")
	}
}

// readAvatarForm reads the avatar form's "file" part: at most
// core.MaxIcon bytes, errIconTooLarge past it.
func readAvatarForm(r *http.Request) ([]byte, error) {
	mr, err := r.MultipartReader()
	if err != nil {
		return nil, err
	}
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			return nil, nil
		}
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			return nil, errIconTooLarge
		}
		if err != nil {
			return nil, err
		}
		if part.FormName() == "file" {
			return readIconFile(part)
		}
	}
}

// profileAction runs a profile change and returns to /profile with okMsg,
// or with a refusal's message, as a toast.
func profileAction(w http.ResponseWriter, r *http.Request, d Deps, okMsg string, fn func() error) {
	err := fn()
	switch inv, ok := errors.AsType[*core.Invalid](err); {
	case err == nil:
		redirectWithFlash(w, r, flash.Success, okMsg, "/profile")
	case ok:
		redirectWithFlash(w, r, flash.Error, inv.Error(), "/profile")
	default:
		serverError(w, r, d, err)
	}
}

// ProfileAvatarSave is POST /profile/avatar: the chosen image becomes the
// owner's avatar, checked as a project icon is.
func ProfileAvatarSave(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		b, err := readAvatarForm(r)
		switch {
		case errors.Is(err, errIconTooLarge):
			redirectWithFlash(w, r, flash.Error, middleware.IconTooLargeMessage, "/profile")
			return
		case err != nil:
			redirectWithFlash(w, r, flash.Error, "That upload couldn't be read.", "/profile")
			return
		case len(b) == 0:
			redirectWithFlash(w, r, flash.Error, "Choose an image to upload.", "/profile")
			return
		}
		profileAction(w, r, d, "Avatar saved.", func() error {
			_, err := d.Core.SetAvatar(r.Context(), d.Actor.ID, "", b)
			return err
		})
	}
}

// ProfileAvatarRemove is POST /profile/avatar/delete, behind the confirm
// dialog: the monogram shows again.
func ProfileAvatarRemove(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		profileAction(w, r, d, "Avatar removed.", func() error {
			_, err := d.Core.RemoveAvatar(r.Context(), d.Actor.ID)
			return err
		})
	}
}

// AvatarServe is GET /avatars/{actor}: the actor's uploaded image, served
// like a project icon (its etag, private, no-cache, so a browser
// revalidates and gets a 304 while it is unchanged; nosniff and a sandbox
// CSP). An actor without one is a 404.
func AvatarServe(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		av, err := d.R.GetActorAvatar(r.Context(), chi.URLParam(r, "actor"))
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			serverError(w, r, d, err)
			return
		}
		serveImage(w, r, d, "actor", av.ActorID, av.ContentType, av.Data, av.Etag)
	}
}
