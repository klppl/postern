-- Postern schema. Single migration for now; future changes go in 0002_*.sql etc.

CREATE TABLE admins (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    username        TEXT    NOT NULL UNIQUE,
    password_hash   TEXT    NOT NULL,
    session_version INTEGER NOT NULL DEFAULT 1,
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE api_keys (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    name             TEXT    NOT NULL,
    key_hash         TEXT    NOT NULL UNIQUE,   -- sha256 of raw key, for lookup
    key_prefix       TEXT    NOT NULL,          -- first 8 chars of raw, for display
    from_address     TEXT    NOT NULL,
    from_name        TEXT    NOT NULL DEFAULT '',
    to_addresses     TEXT    NOT NULL DEFAULT '[]',  -- json array
    cc_addresses     TEXT    NOT NULL DEFAULT '[]',
    bcc_addresses    TEXT    NOT NULL DEFAULT '[]',
    rate_per_minute  INTEGER NOT NULL DEFAULT 0,     -- 0 = unlimited
    rate_per_hour    INTEGER NOT NULL DEFAULT 0,
    rate_per_day     INTEGER NOT NULL DEFAULT 0,
    disabled         INTEGER NOT NULL DEFAULT 0,
    created_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_api_keys_hash ON api_keys(key_hash);

CREATE TABLE templates (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    name        TEXT    NOT NULL UNIQUE,
    subject     TEXT    NOT NULL DEFAULT '',
    body_text   TEXT    NOT NULL DEFAULT '',
    body_html   TEXT    NOT NULL DEFAULT '',
    restricted  INTEGER NOT NULL DEFAULT 0,  -- if 1, must be in api_key_templates
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE api_key_templates (
    api_key_id  INTEGER NOT NULL,
    template_id INTEGER NOT NULL,
    PRIMARY KEY (api_key_id, template_id),
    FOREIGN KEY (api_key_id)  REFERENCES api_keys(id)  ON DELETE CASCADE,
    FOREIGN KEY (template_id) REFERENCES templates(id) ON DELETE CASCADE
);

CREATE TABLE outbox (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    message_id      TEXT    NOT NULL UNIQUE,    -- UUID returned to caller
    api_key_id      INTEGER NOT NULL,
    from_address    TEXT    NOT NULL,
    from_name       TEXT    NOT NULL DEFAULT '',
    to_addresses    TEXT    NOT NULL DEFAULT '[]',
    cc_addresses    TEXT    NOT NULL DEFAULT '[]',
    bcc_addresses   TEXT    NOT NULL DEFAULT '[]',
    subject         TEXT    NOT NULL DEFAULT '',
    body_text       TEXT    NOT NULL DEFAULT '',
    body_html       TEXT    NOT NULL DEFAULT '',
    status          TEXT    NOT NULL DEFAULT 'pending',  -- pending|sending|sent|failed|dead
    attempts        INTEGER NOT NULL DEFAULT 0,
    next_attempt_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_error      TEXT    NOT NULL DEFAULT '',
    smtp_response   TEXT    NOT NULL DEFAULT '',
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    sent_at         DATETIME,
    FOREIGN KEY (api_key_id) REFERENCES api_keys(id) ON DELETE RESTRICT
);
CREATE INDEX idx_outbox_status_next   ON outbox(status, next_attempt_at);
CREATE INDEX idx_outbox_created       ON outbox(created_at);
CREATE INDEX idx_outbox_api_key       ON outbox(api_key_id);

CREATE TABLE outbox_attempts (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    outbox_id     INTEGER NOT NULL,
    attempt_no    INTEGER NOT NULL,
    smtp_response TEXT    NOT NULL DEFAULT '',
    error         TEXT    NOT NULL DEFAULT '',
    attempted_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (outbox_id) REFERENCES outbox(id) ON DELETE CASCADE
);
CREATE INDEX idx_outbox_attempts_outbox ON outbox_attempts(outbox_id);

CREATE TABLE settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL DEFAULT ''
);

-- Default operational settings.
INSERT INTO settings (key, value) VALUES
    ('smtp_host', ''),
    ('smtp_port', '587'),
    ('smtp_username', ''),
    ('smtp_password_enc', ''),
    ('smtp_tls_mode', 'starttls'),    -- none|starttls|tls
    ('retention_days', '90');

CREATE TABLE rate_counters (
    api_key_id   INTEGER NOT NULL,
    bucket       TEXT    NOT NULL,    -- 'minute'|'hour'|'day'
    window_start DATETIME NOT NULL,
    count        INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (api_key_id, bucket)
);
