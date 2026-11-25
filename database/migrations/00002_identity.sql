-- +goose Up
CREATE TABLE app.identity_accounts (
 id text PRIMARY KEY CHECK (id ~ '^[0-9a-f]{32}$'),
 status text NOT NULL DEFAULT 'active' CHECK (status IN ('active','restricted')),
 display_name text NOT NULL DEFAULT '' CHECK (octet_length(display_name)<=256),
 contact_email text NOT NULL DEFAULT '' CHECK (octet_length(contact_email)<=320),
 avatar_url text NOT NULL DEFAULT '' CHECK (octet_length(avatar_url)<=2048),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TABLE app.identity_google (
 provider text NOT NULL DEFAULT 'google' CHECK (provider='google'),
 subject text NOT NULL CHECK (octet_length(subject) BETWEEN 1 AND 255),
 account_id text NOT NULL UNIQUE REFERENCES app.identity_accounts(id),
 PRIMARY KEY(provider,subject)
);
CREATE TABLE app.identity_passwords (
 account_id text PRIMARY KEY REFERENCES app.identity_accounts(id),
 credential text NOT NULL CHECK (octet_length(credential) BETWEEN 1 AND 512)
);
CREATE TABLE app.identity_identifiers (
 account_id text NOT NULL REFERENCES app.identity_accounts(id),
 kind text NOT NULL CHECK (kind IN ('username','phone')),
 value text NOT NULL,
 PRIMARY KEY(kind,value), UNIQUE(account_id,kind),
 CHECK ((kind='username' AND value ~ '^[a-z][a-z0-9_]{2,31}$') OR (kind='phone' AND value ~ '^\+[1-9][0-9]{7,14}$'))
);
CREATE TABLE app.identity_installations (
 id text PRIMARY KEY CHECK (id ~ '^[0-9a-f]{32}$'),
 restricted boolean NOT NULL DEFAULT false
);
CREATE TABLE app.identity_challenges (
 token_hash text PRIMARY KEY CHECK (token_hash ~ '^[0-9a-f]{64}$'),
 installation_id text NOT NULL REFERENCES app.identity_installations(id),
 purpose text NOT NULL CHECK (purpose IN ('login','password','identifiers')),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 expires_at timestamptz NOT NULL, consumed boolean NOT NULL DEFAULT false,
 CHECK (expires_at > created_at)
);
CREATE INDEX identity_challenge_expiry ON app.identity_challenges(expires_at);
CREATE TABLE app.identity_proofs (
 token_hash text PRIMARY KEY CHECK (token_hash ~ '^[0-9a-f]{64}$'),
 account_id text NOT NULL REFERENCES app.identity_accounts(id),
 installation_id text NOT NULL REFERENCES app.identity_installations(id),
 purpose text NOT NULL CHECK (purpose IN ('password','identifiers')),
 expires_at timestamptz NOT NULL, consumed boolean NOT NULL DEFAULT false
);
CREATE INDEX identity_proof_expiry ON app.identity_proofs(expires_at);
CREATE TABLE app.identity_sessions (
 id text PRIMARY KEY CHECK (id ~ '^[0-9a-f]{32}$'),
 account_id text NOT NULL REFERENCES app.identity_accounts(id),
 installation_id text NOT NULL REFERENCES app.identity_installations(id),
 access_hash text NOT NULL UNIQUE CHECK (access_hash ~ '^[0-9a-f]{64}$'),
 access_expires_at timestamptz NOT NULL,
 expires_at timestamptz NOT NULL,
 authenticated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 revoked boolean NOT NULL DEFAULT false,
 CHECK(access_expires_at<=expires_at)
);
CREATE INDEX identity_account_sessions ON app.identity_sessions(account_id,authenticated_at,id) WHERE NOT revoked;
CREATE INDEX identity_session_expiry ON app.identity_sessions(expires_at);
CREATE INDEX identity_installation_sessions ON app.identity_sessions(installation_id);
CREATE TABLE app.identity_refresh_tokens (
 token_hash text PRIMARY KEY CHECK (token_hash ~ '^[0-9a-f]{64}$'),
 session_id text NOT NULL REFERENCES app.identity_sessions(id),
 consumed boolean NOT NULL DEFAULT false
);
CREATE INDEX identity_refresh_session ON app.identity_refresh_tokens(session_id);
CREATE TABLE app.identity_rate_limits (
 scope_hash text PRIMARY KEY CHECK (scope_hash ~ '^[0-9a-f]{64}$'),
 window_at timestamptz NOT NULL, attempts integer NOT NULL CHECK(attempts>0)
);
CREATE INDEX identity_rate_expiry ON app.identity_rate_limits(window_at);
-- Runtime role cannot create schema or access migration credentials/history.
-- +goose StatementBegin
DO $$
DECLARE runtime_role text := current_database() || '_app';
BEGIN
 EXECUTE format('GRANT SELECT,INSERT,UPDATE,DELETE ON app.identity_accounts,app.identity_google,app.identity_passwords,app.identity_identifiers,app.identity_installations,app.identity_challenges,app.identity_proofs,app.identity_sessions,app.identity_refresh_tokens,app.identity_rate_limits TO %I',runtime_role);
END $$;
-- +goose StatementEnd
-- +goose Down
DROP TABLE app.identity_rate_limits;
DROP TABLE app.identity_refresh_tokens;
DROP TABLE app.identity_sessions;
DROP TABLE app.identity_proofs;
DROP TABLE app.identity_challenges;
DROP TABLE app.identity_installations;
DROP TABLE app.identity_identifiers;
DROP TABLE app.identity_passwords;
DROP TABLE app.identity_google;
DROP TABLE app.identity_accounts;
