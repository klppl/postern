// Package templates renders Handlebars-style email templates.
//
// We compile templates on every render rather than caching: templates are
// short, the call rate is low (one per outbound email), and avoiding a
// cache means changes from the admin UI take effect immediately without
// any invalidation logic.
package templates

import (
	"fmt"

	"github.com/aymerick/raymond"
)

// Rendered holds the substituted output from a template.
type Rendered struct {
	Subject  string
	BodyText string
	BodyHTML string
}

// Render applies vars to the subject and bodies.
func Render(subject, bodyText, bodyHTML string, vars map[string]any) (Rendered, error) {
	subj, err := renderOne("subject", subject, vars)
	if err != nil {
		return Rendered{}, err
	}
	txt, err := renderOne("body_text", bodyText, vars)
	if err != nil {
		return Rendered{}, err
	}
	html, err := renderOne("body_html", bodyHTML, vars)
	if err != nil {
		return Rendered{}, err
	}
	return Rendered{Subject: subj, BodyText: txt, BodyHTML: html}, nil
}

func renderOne(label, src string, vars map[string]any) (string, error) {
	if src == "" {
		return "", nil
	}
	out, err := raymond.Render(src, vars)
	if err != nil {
		return "", fmt.Errorf("%s: %w", label, err)
	}
	return out, nil
}
