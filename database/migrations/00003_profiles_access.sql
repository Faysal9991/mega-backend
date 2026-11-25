-- +goose Up
CREATE TABLE app.profiles (
 account_id text PRIMARY KEY REFERENCES app.identity_accounts(id) ON DELETE CASCADE,
 display_name text NOT NULL CHECK(octet_length(display_name)<=256),
 bio text NOT NULL DEFAULT '' CHECK(octet_length(bio)<=2048),
 avatar_url text NOT NULL CHECK(octet_length(avatar_url)<=2048)
);
INSERT INTO app.profiles(account_id,display_name,avatar_url) SELECT id,display_name,avatar_url FROM app.identity_accounts;
ALTER TABLE app.identity_accounts DROP COLUMN display_name, DROP COLUMN avatar_url;
CREATE TABLE app.access_owners(account_id text PRIMARY KEY REFERENCES app.identity_accounts(id));
CREATE TABLE app.access_roles (
 id text PRIMARY KEY CHECK(id ~ '^[0-9a-f]{32}$'),
 name text NOT NULL UNIQUE CHECK(octet_length(name) BETWEEN 1 AND 64)
);
CREATE TABLE app.access_role_permissions (
 role_id text NOT NULL REFERENCES app.access_roles(id) ON DELETE CASCADE,
 permission text NOT NULL CHECK(permission IN ('users.read','users.private.read','users.profile.update','users.restrict')),
 PRIMARY KEY(role_id,permission)
);
CREATE TABLE app.access_assignments (
 account_id text NOT NULL REFERENCES app.identity_accounts(id),
 role_id text NOT NULL REFERENCES app.access_roles(id) ON DELETE CASCADE,
 PRIMARY KEY(account_id,role_id)
);
CREATE INDEX access_assignments_role ON app.access_assignments(role_id,account_id);
CREATE TABLE app.access_audit (
 id text PRIMARY KEY CHECK(id ~ '^[0-9a-f]{32}$'),
 actor text NOT NULL REFERENCES app.identity_accounts(id),
 target text NOT NULL,
 action text NOT NULL CHECK(octet_length(action) BETWEEN 1 AND 64),
 fields text[] NOT NULL CHECK(cardinality(fields)<=104),
 reason text NOT NULL CHECK(octet_length(reason) BETWEEN 1 AND 256),
 correlation text NOT NULL CHECK(correlation ~ '^[A-Za-z0-9_-]{1,64}$'),
 occurred_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
-- +goose StatementBegin
DO $$
DECLARE runtime_role text := current_database() || '_app';
BEGIN
 EXECUTE format('GRANT SELECT,INSERT,UPDATE,DELETE ON app.access_roles,app.access_role_permissions,app.access_assignments TO %I',runtime_role);
 EXECUTE format('GRANT SELECT,INSERT,UPDATE ON app.profiles TO %I',runtime_role);
 EXECUTE format('GRANT SELECT ON app.access_owners TO %I',runtime_role);
 EXECUTE format('GRANT INSERT ON app.access_audit TO %I',runtime_role);
END $$;
-- +goose StatementEnd
-- +goose Down
ALTER TABLE app.identity_accounts ADD COLUMN display_name text NOT NULL DEFAULT '' CHECK(octet_length(display_name)<=256), ADD COLUMN avatar_url text NOT NULL DEFAULT '' CHECK(octet_length(avatar_url)<=2048);
UPDATE app.identity_accounts a SET display_name=p.display_name,avatar_url=p.avatar_url FROM app.profiles p WHERE a.id=p.account_id;
DROP TABLE app.access_audit;
DROP TABLE app.access_assignments;
DROP TABLE app.access_role_permissions;
DROP TABLE app.access_roles;
DROP TABLE app.access_owners;
DROP TABLE app.profiles;
