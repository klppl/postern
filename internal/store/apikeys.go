package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

type APIKey struct {
	ID                     int64
	Name                   string
	KeyHash                string
	KeyPrefix              string
	FromAddress            string
	FromName               string
	ToAddresses            []string
	CcAddresses            []string
	BccAddresses           []string
	RatePerMinute          int
	RatePerHour            int
	RatePerDay             int
	Disabled               bool
	AllowRequestRecipients bool
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

func scanAPIKey(s rowScanner) (*APIKey, error) {
	var k APIKey
	var to, cc, bcc string
	var disabled, allowReqRcpt int
	if err := s.Scan(
		&k.ID, &k.Name, &k.KeyHash, &k.KeyPrefix,
		&k.FromAddress, &k.FromName,
		&to, &cc, &bcc,
		&k.RatePerMinute, &k.RatePerHour, &k.RatePerDay,
		&disabled, &allowReqRcpt, &k.CreatedAt, &k.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(to), &k.ToAddresses); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(cc), &k.CcAddresses); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(bcc), &k.BccAddresses); err != nil {
		return nil, err
	}
	k.Disabled = disabled != 0
	k.AllowRequestRecipients = allowReqRcpt != 0
	return &k, nil
}

const apiKeyCols = `id, name, key_hash, key_prefix, from_address, from_name,
		to_addresses, cc_addresses, bcc_addresses,
		rate_per_minute, rate_per_hour, rate_per_day,
		disabled, allow_request_recipients, created_at, updated_at`

func (s *Store) GetAPIKeyByHash(ctx context.Context, hash string) (*APIKey, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+apiKeyCols+` FROM api_keys WHERE key_hash = ?`, hash)
	k, err := scanAPIKey(row)
	if err != nil {
		return nil, wrapNotFound(err)
	}
	return k, nil
}

func (s *Store) GetAPIKey(ctx context.Context, id int64) (*APIKey, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+apiKeyCols+` FROM api_keys WHERE id = ?`, id)
	k, err := scanAPIKey(row)
	if err != nil {
		return nil, wrapNotFound(err)
	}
	return k, nil
}

func (s *Store) ListAPIKeys(ctx context.Context) ([]*APIKey, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+apiKeyCols+` FROM api_keys ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*APIKey
	for rows.Next() {
		k, err := scanAPIKey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

func (s *Store) CreateAPIKey(ctx context.Context, k *APIKey) (int64, error) {
	to, _ := json.Marshal(stringsOrEmpty(k.ToAddresses))
	cc, _ := json.Marshal(stringsOrEmpty(k.CcAddresses))
	bcc, _ := json.Marshal(stringsOrEmpty(k.BccAddresses))
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO api_keys
		 (name, key_hash, key_prefix, from_address, from_name,
		  to_addresses, cc_addresses, bcc_addresses,
		  rate_per_minute, rate_per_hour, rate_per_day, disabled, allow_request_recipients)
		 VALUES (?,?,?,?,?, ?,?,?, ?,?,?, ?, ?)`,
		k.Name, k.KeyHash, k.KeyPrefix, k.FromAddress, k.FromName,
		string(to), string(cc), string(bcc),
		k.RatePerMinute, k.RatePerHour, k.RatePerDay,
		boolInt(k.Disabled), boolInt(k.AllowRequestRecipients),
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) UpdateAPIKey(ctx context.Context, k *APIKey) error {
	to, _ := json.Marshal(stringsOrEmpty(k.ToAddresses))
	cc, _ := json.Marshal(stringsOrEmpty(k.CcAddresses))
	bcc, _ := json.Marshal(stringsOrEmpty(k.BccAddresses))
	_, err := s.db.ExecContext(ctx,
		`UPDATE api_keys SET
		    name = ?, from_address = ?, from_name = ?,
		    to_addresses = ?, cc_addresses = ?, bcc_addresses = ?,
		    rate_per_minute = ?, rate_per_hour = ?, rate_per_day = ?,
		    disabled = ?, allow_request_recipients = ?,
		    updated_at = CURRENT_TIMESTAMP
		 WHERE id = ?`,
		k.Name, k.FromAddress, k.FromName,
		string(to), string(cc), string(bcc),
		k.RatePerMinute, k.RatePerHour, k.RatePerDay,
		boolInt(k.Disabled), boolInt(k.AllowRequestRecipients),
		k.ID,
	)
	return err
}

// RotateAPIKey replaces the hash + prefix in place; the surrounding caller
// is expected to display the new raw key once.
func (s *Store) RotateAPIKey(ctx context.Context, id int64, newHash, newPrefix string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE api_keys SET key_hash = ?, key_prefix = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		newHash, newPrefix, id)
	return err
}

func (s *Store) DeleteAPIKey(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM api_keys WHERE id = ?`, id)
	return err
}

// SetAllowedTemplates replaces the entire allow-list for a key. Pass an
// empty slice to allow no restricted templates.
func (s *Store) SetAllowedTemplates(ctx context.Context, apiKeyID int64, templateIDs []int64) error {
	return s.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM api_key_templates WHERE api_key_id = ?`, apiKeyID); err != nil {
			return err
		}
		stmt, err := tx.PrepareContext(ctx, `INSERT INTO api_key_templates (api_key_id, template_id) VALUES (?, ?)`)
		if err != nil {
			return err
		}
		defer stmt.Close()
		for _, tid := range templateIDs {
			if _, err := stmt.ExecContext(ctx, apiKeyID, tid); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Store) AllowedTemplateIDs(ctx context.Context, apiKeyID int64) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT template_id FROM api_key_templates WHERE api_key_id = ?`, apiKeyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func stringsOrEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
