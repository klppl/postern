-- Per-key opt-in: when set, the API request may carry to/cc/bcc that
-- override the key's defaults. When unset (default), recipients are still
-- strictly bound to the key.
ALTER TABLE api_keys ADD COLUMN allow_request_recipients INTEGER NOT NULL DEFAULT 0;
