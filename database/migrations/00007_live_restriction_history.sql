-- +goose Up
CREATE TABLE app.live_account_restrictions (
 id text PRIMARY KEY CHECK(id ~ '^[0-9a-f]{32}$'),
 account_id text NOT NULL REFERENCES app.identity_accounts(id),
 restricted_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX live_account_restriction_window ON app.live_account_restrictions(account_id,restricted_at,id);
CREATE INDEX live_members_room_active ON app.live_members(room_id,account_id) WHERE state='active';
-- +goose StatementBegin
DO $$ DECLARE r text := current_database() || '_app'; BEGIN
 EXECUTE format('GRANT SELECT,INSERT ON app.live_account_restrictions TO %I',r);
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN IF EXISTS(SELECT 1 FROM app.live_account_restrictions) THEN RAISE EXCEPTION 'restriction history rollback requires reviewed recovery'; END IF; END $$;
-- +goose StatementEnd
DROP INDEX IF EXISTS app.live_members_room_active;
DROP TABLE app.live_account_restrictions;
