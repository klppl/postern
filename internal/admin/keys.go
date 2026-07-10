package admin

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/alexander/postern/internal/auth"
	"github.com/alexander/postern/internal/store"
	"github.com/go-chi/chi/v5"
)

type keyListData struct {
	Keys []*store.APIKey
}

type keyFormData struct {
	Key        *store.APIKey
	NewRawKey  string // populated only on create / rotate
	BaseURL    string // populated alongside NewRawKey for the curl snippet
	Templates  []*store.Template
	AllowedIDs []int64
}

func (s *Server) listKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := s.store.ListAPIKeys(r.Context())
	if err != nil {
		s.log.Error("list keys", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	s.render(w, r, "apikeys.html", "API keys", "keys", keyListData{Keys: keys})
}

func (s *Server) newKey(w http.ResponseWriter, r *http.Request) {
	tmpls, err := s.store.ListTemplates(r.Context())
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	s.render(w, r, "apikey_form.html", "New API key", "keys", keyFormData{
		Key:       &store.APIKey{},
		Templates: tmpls,
	})
}

func (s *Server) createKey(w http.ResponseWriter, r *http.Request) {
	k, allowed, err := parseKeyForm(r)
	if err != nil {
		s.flashError(w, err.Error())
		http.Redirect(w, r, "/admin/keys/new", http.StatusSeeOther)
		return
	}

	raw, hash, prefix, err := auth.IssueAPIKey()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	k.KeyHash = hash
	k.KeyPrefix = prefix

	id, err := s.store.CreateAPIKey(r.Context(), k)
	if err != nil {
		s.flashError(w, "could not create key: "+err.Error())
		http.Redirect(w, r, "/admin/keys/new", http.StatusSeeOther)
		return
	}
	if err := s.store.SetAllowedTemplates(r.Context(), id, allowed); err != nil {
		s.log.Error("set allowed templates", "err", err)
	}

	tmpls, _ := s.store.ListTemplates(r.Context())
	k.ID = id
	s.render(w, r, "apikey_form.html", "API key created", "keys", keyFormData{
		Key:        k,
		NewRawKey:  raw,
		BaseURL:    deriveBaseURL(r),
		Templates:  tmpls,
		AllowedIDs: allowed,
	})
}

func (s *Server) editKey(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	k, err := s.store.GetAPIKey(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	tmpls, err := s.store.ListTemplates(r.Context())
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	allowed, _ := s.store.AllowedTemplateIDs(r.Context(), id)
	s.render(w, r, "apikey_form.html", "Edit API key", "keys", keyFormData{
		Key: k, Templates: tmpls, AllowedIDs: allowed,
	})
}

func (s *Server) updateKey(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	k, allowed, err := parseKeyForm(r)
	if err != nil {
		s.flashError(w, err.Error())
		http.Redirect(w, r, "/admin/keys/"+chi.URLParam(r, "id"), http.StatusSeeOther)
		return
	}
	k.ID = id
	if err := s.store.UpdateAPIKey(r.Context(), k); err != nil {
		s.flashError(w, "update failed: "+err.Error())
		http.Redirect(w, r, "/admin/keys/"+chi.URLParam(r, "id"), http.StatusSeeOther)
		return
	}
	if err := s.store.SetAllowedTemplates(r.Context(), id, allowed); err != nil {
		s.log.Error("set allowed templates", "err", err)
	}
	s.flashInfo(w, "Key updated.")
	http.Redirect(w, r, "/admin/keys", http.StatusSeeOther)
}

func (s *Server) rotateKey(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	raw, hash, prefix, err := auth.IssueAPIKey()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if err := s.store.RotateAPIKey(r.Context(), id, hash, prefix); err != nil {
		s.flashError(w, "rotate failed: "+err.Error())
		http.Redirect(w, r, "/admin/keys/"+chi.URLParam(r, "id"), http.StatusSeeOther)
		return
	}
	k, _ := s.store.GetAPIKey(r.Context(), id)
	tmpls, _ := s.store.ListTemplates(r.Context())
	allowed, _ := s.store.AllowedTemplateIDs(r.Context(), id)
	s.render(w, r, "apikey_form.html", "API key rotated", "keys", keyFormData{
		Key: k, NewRawKey: raw, BaseURL: deriveBaseURL(r), Templates: tmpls, AllowedIDs: allowed,
	})
}

func (s *Server) deleteKey(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := s.store.DeleteAPIKey(r.Context(), id); err != nil {
		s.flashError(w, "delete failed: "+err.Error())
	} else {
		s.flashInfo(w, "Key deleted.")
	}
	http.Redirect(w, r, "/admin/keys", http.StatusSeeOther)
}

func parseKeyForm(r *http.Request) (*store.APIKey, []int64, error) {
	if err := r.ParseForm(); err != nil {
		return nil, nil, err
	}
	k := &store.APIKey{
		Name:                   strings.TrimSpace(r.FormValue("name")),
		FromAddress:            strings.TrimSpace(r.FormValue("from_address")),
		FromName:               strings.TrimSpace(r.FormValue("from_name")),
		ToAddresses:            splitLines(r.FormValue("to_addresses")),
		CcAddresses:            splitLines(r.FormValue("cc_addresses")),
		BccAddresses:           splitLines(r.FormValue("bcc_addresses")),
		RatePerMinute:          atoi(r.FormValue("rate_per_minute")),
		RatePerHour:            atoi(r.FormValue("rate_per_hour")),
		RatePerDay:             atoi(r.FormValue("rate_per_day")),
		Disabled:               r.FormValue("disabled") == "on",
		AllowRequestRecipients: r.FormValue("allow_request_recipients") == "on",
	}
	if k.Name == "" {
		return nil, nil, ErrFormValidation("name is required")
	}
	if k.FromAddress == "" {
		return nil, nil, ErrFormValidation("from_address is required")
	}
	// When the key allows request-supplied recipients, the default list may
	// legitimately be empty (every send will carry its own recipients).
	if len(k.ToAddresses) == 0 && !k.AllowRequestRecipients {
		return nil, nil, ErrFormValidation("at least one default recipient is required (or enable request-supplied recipients)")
	}

	var allowed []int64
	for _, v := range r.Form["allowed_template_ids"] {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			allowed = append(allowed, id)
		}
	}
	return k, allowed, nil
}

type ErrFormValidation string

func (e ErrFormValidation) Error() string { return string(e) }

func splitLines(s string) []string {
	parts := strings.Split(s, "\n")
	out := []string{}
	for _, p := range parts {
		t := strings.TrimSpace(strings.TrimRight(p, "\r"))
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}

func atoi(s string) int {
	if s == "" {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	if n < 0 {
		return 0
	}
	return n
}
