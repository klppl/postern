package store

import (
	"context"
	"time"
)

type Template struct {
	ID         int64
	Name       string
	Subject    string
	BodyText   string
	BodyHTML   string
	Restricted bool
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

func scanTemplate(s rowScanner) (*Template, error) {
	var t Template
	var restricted int
	if err := s.Scan(&t.ID, &t.Name, &t.Subject, &t.BodyText, &t.BodyHTML, &restricted, &t.CreatedAt, &t.UpdatedAt); err != nil {
		return nil, err
	}
	t.Restricted = restricted != 0
	return &t, nil
}

const templateCols = `id, name, subject, body_text, body_html, restricted, created_at, updated_at`

func (s *Store) GetTemplate(ctx context.Context, id int64) (*Template, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+templateCols+` FROM templates WHERE id = ?`, id)
	t, err := scanTemplate(row)
	if err != nil {
		return nil, wrapNotFound(err)
	}
	return t, nil
}

func (s *Store) GetTemplateByName(ctx context.Context, name string) (*Template, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+templateCols+` FROM templates WHERE name = ?`, name)
	t, err := scanTemplate(row)
	if err != nil {
		return nil, wrapNotFound(err)
	}
	return t, nil
}

func (s *Store) ListTemplates(ctx context.Context) ([]*Template, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+templateCols+` FROM templates ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Template
	for rows.Next() {
		t, err := scanTemplate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) CreateTemplate(ctx context.Context, t *Template) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO templates (name, subject, body_text, body_html, restricted) VALUES (?, ?, ?, ?, ?)`,
		t.Name, t.Subject, t.BodyText, t.BodyHTML, boolInt(t.Restricted))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) UpdateTemplate(ctx context.Context, t *Template) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE templates SET name = ?, subject = ?, body_text = ?, body_html = ?, restricted = ?, updated_at = CURRENT_TIMESTAMP
		 WHERE id = ?`,
		t.Name, t.Subject, t.BodyText, t.BodyHTML, boolInt(t.Restricted), t.ID)
	return err
}

func (s *Store) DeleteTemplate(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM templates WHERE id = ?`, id)
	return err
}
