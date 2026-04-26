package admin

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/alexander/bifrost/internal/store"
	"github.com/alexander/bifrost/internal/templates"
	"github.com/go-chi/chi/v5"
)

type templateListData struct {
	Templates []*store.Template
}

type templateFormData struct {
	Template *store.Template
	Preview  *templates.Rendered
	Vars     string // raw JSON for the preview pane
}

func (s *Server) listTemplates(w http.ResponseWriter, r *http.Request) {
	t, err := s.store.ListTemplates(r.Context())
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	s.render(w, r, "templates.html", "Templates", "templates", templateListData{Templates: t})
}

func (s *Server) newTemplate(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, "template_form.html", "New template", "templates", templateFormData{
		Template: &store.Template{},
	})
}

func (s *Server) createTemplate(w http.ResponseWriter, r *http.Request) {
	t, err := parseTemplateForm(r)
	if err != nil {
		s.flashError(w, err.Error())
		http.Redirect(w, r, "/admin/templates/new", http.StatusSeeOther)
		return
	}
	id, err := s.store.CreateTemplate(r.Context(), t)
	if err != nil {
		s.flashError(w, "create failed: "+err.Error())
		http.Redirect(w, r, "/admin/templates/new", http.StatusSeeOther)
		return
	}
	s.flashInfo(w, "Template created.")
	http.Redirect(w, r, "/admin/templates/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

func (s *Server) editTemplate(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	t, err := s.store.GetTemplate(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	s.render(w, r, "template_form.html", "Edit template", "templates", templateFormData{Template: t})
}

func (s *Server) updateTemplate(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	t, err := parseTemplateForm(r)
	if err != nil {
		s.flashError(w, err.Error())
		http.Redirect(w, r, "/admin/templates/"+chi.URLParam(r, "id"), http.StatusSeeOther)
		return
	}
	t.ID = id
	if err := s.store.UpdateTemplate(r.Context(), t); err != nil {
		s.flashError(w, "update failed: "+err.Error())
	} else {
		s.flashInfo(w, "Template updated.")
	}
	http.Redirect(w, r, "/admin/templates/"+chi.URLParam(r, "id"), http.StatusSeeOther)
}

func (s *Server) deleteTemplate(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := s.store.DeleteTemplate(r.Context(), id); err != nil {
		s.flashError(w, "delete failed: "+err.Error())
	} else {
		s.flashInfo(w, "Template deleted.")
	}
	http.Redirect(w, r, "/admin/templates", http.StatusSeeOther)
}

// previewTemplate is invoked via HTMX from the template form. Renders with
// the user-supplied JSON variables and returns the rendered fragment.
func (s *Server) previewTemplate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	subject := r.FormValue("subject")
	bodyText := r.FormValue("body_text")
	bodyHTML := r.FormValue("body_html")
	rawVars := r.FormValue("variables")
	vars := map[string]any{}
	if strings.TrimSpace(rawVars) != "" {
		if err := json.Unmarshal([]byte(rawVars), &vars); err != nil {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write([]byte(`<div class="error">Variables must be valid JSON: ` + jsonEscapeHTML(err.Error()) + `</div>`))
			return
		}
	}
	rendered, err := templates.Render(subject, bodyText, bodyHTML, vars)
	if err != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(`<div class="error">Render error: ` + jsonEscapeHTML(err.Error()) + `</div>`))
		return
	}
	s.renderPartial(w, "template_preview.html", rendered)
}

func parseTemplateForm(r *http.Request) (*store.Template, error) {
	if err := r.ParseForm(); err != nil {
		return nil, err
	}
	t := &store.Template{
		Name:       strings.TrimSpace(r.FormValue("name")),
		Subject:    r.FormValue("subject"),
		BodyText:   r.FormValue("body_text"),
		BodyHTML:   r.FormValue("body_html"),
		Restricted: r.FormValue("restricted") == "on",
	}
	if t.Name == "" {
		return nil, ErrFormValidation("name is required")
	}
	return t, nil
}

func jsonEscapeHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}
