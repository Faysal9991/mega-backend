-- +goose Up
CREATE TABLE app.agency_activity_contexts (
 id text PRIMARY KEY CHECK(id ~ '^[0-9a-f]{32}$'),
 revision bigint GENERATED ALWAYS AS IDENTITY UNIQUE,
 account_id text NOT NULL REFERENCES app.identity_accounts(id),
 agency_id text REFERENCES app.identity_accounts(id),
 started_at timestamptz NOT NULL,
 recorded_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX agency_activity_context_latest ON app.agency_activity_contexts(account_id,revision DESC);
-- The pre-batch Wallet association is compatible, but no verified LiveKit
-- collector existed at baseline 645c75c. Preserve its current effective floor.
INSERT INTO app.agency_activity_contexts(id,account_id,agency_id,started_at)
SELECT md5('initial-agency-activity-context:'||id),account_id,agency_id,effective_from
FROM app.agency_host_associations WHERE effective_until IS NULL;
-- +goose StatementBegin
DO $$ DECLARE r text := current_database() || '_app'; BEGIN
 EXECUTE format('GRANT SELECT,INSERT ON app.agency_activity_contexts TO %I',r);
 EXECUTE format('GRANT USAGE ON SEQUENCE app.agency_activity_contexts_revision_seq TO %I',r);
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN IF EXISTS(SELECT 1 FROM app.agency_activity_contexts) THEN RAISE EXCEPTION 'activity context rollback requires reviewed recovery'; END IF; END $$;
-- +goose StatementEnd
DROP TABLE app.agency_activity_contexts;
