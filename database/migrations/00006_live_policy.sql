-- +goose Up
CREATE TABLE app.live_policies (
 id text PRIMARY KEY, previous_id text NOT NULL, actor_id text NOT NULL REFERENCES app.identity_accounts(id),
 revision bigint GENERATED ALWAYS AS IDENTITY UNIQUE, config jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX live_presence_account_sid ON app.live_presence(account_id,participant_sid);
CREATE INDEX agency_current_agency ON app.agency_host_associations(agency_id,account_id) WHERE effective_until IS NULL;
CREATE INDEX live_current_publishers ON app.live_members(room_id,account_id) WHERE state='active' AND (role IN ('host','co_host') OR seat IS NOT NULL);
-- +goose StatementBegin
DO $$ DECLARE r text := current_database() || '_app'; BEGIN
 EXECUTE format('GRANT SELECT,INSERT ON app.live_policies TO %I',r);
 EXECUTE format('GRANT USAGE ON SEQUENCE app.live_policies_revision_seq TO %I',r);
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN IF EXISTS(SELECT 1 FROM app.live_policies) THEN RAISE EXCEPTION 'policy rollback requires reviewed data recovery'; END IF; END $$;
-- +goose StatementEnd
DROP INDEX IF EXISTS app.live_current_publishers;
DROP INDEX IF EXISTS app.agency_current_agency;
DROP INDEX IF EXISTS app.live_presence_account_sid;
DROP TABLE app.live_policies;
