package templates

import "testing"

func TestRenderBasic(t *testing.T) {
	r, err := Render("Hello {{name}}", "Hi {{name}}", "<p>Hi {{name}}</p>", map[string]any{"name": "Alex"})
	if err != nil {
		t.Fatal(err)
	}
	if r.Subject != "Hello Alex" {
		t.Errorf("subject: %q", r.Subject)
	}
	if r.BodyText != "Hi Alex" {
		t.Errorf("text: %q", r.BodyText)
	}
	if r.BodyHTML != "<p>Hi Alex</p>" {
		t.Errorf("html: %q", r.BodyHTML)
	}
}

func TestRenderEmptyParts(t *testing.T) {
	r, err := Render("subj", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.Subject != "subj" || r.BodyText != "" || r.BodyHTML != "" {
		t.Fatalf("unexpected: %#v", r)
	}
}

func TestRenderInvalidTemplate(t *testing.T) {
	_, err := Render("{{unclosed", "", "", nil)
	if err == nil {
		t.Fatal("expected error on malformed handlebars")
	}
}

func TestRenderMissingVar(t *testing.T) {
	// Raymond renders missing vars as empty without error — verify that
	// behavior so we know what callers will see.
	r, err := Render("hi {{name}}", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.Subject != "hi " {
		t.Errorf("got %q", r.Subject)
	}
}
