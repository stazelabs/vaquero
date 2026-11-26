package handlers

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/stazelabs/vaquero/internal/api"
	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/web/middleware"
	"github.com/stazelabs/vaquero/internal/web/templates"
)

// writeProfile answers the owner's profile, read afresh.
func writeProfile(w http.ResponseWriter, r *http.Request, d Deps) {
	a, err := d.R.GetActor(r.Context(), d.Actor.ID)
	if err != nil {
		apiFail(w, r, d, api.Internal(), err)
		return
	}
	out := api.Profile{Name: a.Name, DisplayName: a.DisplayName, Shown: core.ActorName(a)}
	etag, err := d.R.GetActorAvatarEtag(r.Context(), a.ID)
	switch {
	case err == nil:
		out.Avatar = &api.ProfileAvatar{URL: templates.AvatarURL(a.ID), Etag: etag}
	case !errors.Is(err, sql.ErrNoRows):
		apiFail(w, r, d, api.Internal(), err)
		return
	}
	api.WriteJSON(w, http.StatusOK, out)
}

// profileFail answers a refused or failed profile change.
func profileFail(w http.ResponseWriter, r *http.Request, d Deps, err error) {
	apiFail(w, r, d, api.FromCore(err, func(*core.Conflict) any { return nil }), err)
}

// APIProfile is GET /api/v1/profile: the owner's profile (VAQ-149).
func APIProfile(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { writeProfile(w, r, d) }
}

// APIProfileUpdate is PATCH /api/v1/profile: {"display_name"} ("" shows
// Owner again). Only the owner, acting directly, edits it: an agent is
// refused with 422.
func APIProfileUpdate(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			DisplayName *string `json:"display_name"`
		}
		if f := api.Decode(r, &in); f != nil {
			api.WriteFailure(w, f)
			return
		}
		if _, err := d.Core.UpdateProfile(r.Context(), apiActor(r).ID, in.DisplayName); err != nil {
			profileFail(w, r, d, err)
			return
		}
		writeProfile(w, r, d)
	}
}

// APIProfileAvatarUpload is PUT /api/v1/profile/avatar: the multipart part
// "file" becomes the owner's avatar (PNG, JPEG or WebP, at most 256 KB,
// sniffed). It answers the profile.
func APIProfileAvatarUpload(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		part, fail := uploadedPart(r)
		if fail != nil {
			if fail.Status == http.StatusRequestEntityTooLarge {
				fail = middleware.TooLargeIcon()
			}
			api.WriteFailure(w, fail)
			return
		}
		b, err := readIconFile(part)
		switch {
		case errors.Is(err, errIconTooLarge):
			api.WriteFailure(w, middleware.TooLargeIcon())
			return
		case err != nil:
			api.WriteFailure(w, api.Fail(http.StatusBadRequest, "bad_request", "That upload couldn't be read."))
			return
		}
		if _, err := d.Core.SetAvatar(r.Context(), apiActor(r).ID, part.Header.Get("Content-Type"), b); err != nil {
			profileFail(w, r, d, err)
			return
		}
		writeProfile(w, r, d)
	}
}

// APIProfileAvatarDelete is DELETE /api/v1/profile/avatar: the monogram
// shows again. With none stored nothing changes. It answers the profile.
func APIProfileAvatarDelete(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, err := d.Core.RemoveAvatar(r.Context(), apiActor(r).ID); err != nil {
			profileFail(w, r, d, err)
			return
		}
		writeProfile(w, r, d)
	}
}
