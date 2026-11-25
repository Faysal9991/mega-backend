-- +goose Up
ALTER TABLE app.access_role_permissions DROP CONSTRAINT access_role_permissions_permission_check;
ALTER TABLE app.access_role_permissions ADD CONSTRAINT access_role_permissions_permission_check CHECK(permission IN ('users.read','users.private.read','users.profile.update','users.restrict','economy.policies.read','economy.policies.write','wallet.operations.read','wallet.adjust','withdrawals.review','agency.hosts.manage','agencies.manage','live.rooms.moderate','live.activity.read','live.policies.manage'));
CREATE TABLE app.agencies (
 id text PRIMARY KEY REFERENCES app.identity_accounts(id),
 name text NOT NULL CHECK(octet_length(name) BETWEEN 1 AND 128),
 status text NOT NULL CHECK(status IN ('pending','active','restricted','rejected')),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TABLE app.agency_managers (
 agency_id text NOT NULL REFERENCES app.agencies(id), account_id text NOT NULL REFERENCES app.identity_accounts(id),
 PRIMARY KEY(agency_id,account_id), CHECK(agency_id<>account_id)
);
CREATE TABLE app.host_applications (
 id text PRIMARY KEY, agency_id text NOT NULL REFERENCES app.agencies(id), account_id text NOT NULL REFERENCES app.identity_accounts(id),
 classification text NOT NULL CHECK(classification IN ('audio_host','video_host')),
 state text NOT NULL CHECK(state IN ('pending','approved','rejected','cancelled')),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(), decided_at timestamptz,
 CHECK(agency_id<>account_id)
);
CREATE UNIQUE INDEX host_application_pending ON app.host_applications(account_id) WHERE state='pending';
CREATE INDEX host_application_agency_page ON app.host_applications(agency_id,id);
CREATE TABLE app.live_rooms (
 id text PRIMARY KEY, session_id text NOT NULL UNIQUE, provider_sid text NOT NULL DEFAULT '', owner_id text NOT NULL REFERENCES app.identity_accounts(id),
 kind text NOT NULL CHECK(kind IN ('audio','video')), title text NOT NULL CHECK(octet_length(title) BETWEEN 1 AND 128),
 state text NOT NULL CHECK(state IN ('opening','open','closing','closed')), capacity integer NOT NULL CHECK(capacity BETWEEN 2 AND 500),
 seats integer NOT NULL CHECK(seats BETWEEN 0 AND 32), created_at timestamptz NOT NULL DEFAULT clock_timestamp(), closed_at timestamptz
);
CREATE UNIQUE INDEX live_owner_current ON app.live_rooms(owner_id) WHERE state<>'closed';
CREATE INDEX live_room_page ON app.live_rooms(id) WHERE state='open';
CREATE TABLE app.live_members (
 room_id text NOT NULL REFERENCES app.live_rooms(id), account_id text NOT NULL REFERENCES app.identity_accounts(id),
 role text NOT NULL CHECK(role IN ('host','co_host','audience','moderator')),
 state text NOT NULL CHECK(state IN ('active','left','kicked','banned')), muted boolean NOT NULL DEFAULT false,
 seat integer CHECK(seat BETWEEN 1 AND 32), invited_seat integer CHECK(invited_seat BETWEEN 1 AND 32),
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp(), PRIMARY KEY(room_id,account_id)
);
CREATE UNIQUE INDEX live_seat_occupied ON app.live_members(room_id,seat) WHERE seat IS NOT NULL AND state='active';
CREATE INDEX live_member_account ON app.live_members(account_id,room_id) WHERE state='active';
CREATE TABLE app.live_operations (
 id text PRIMARY KEY, sequence bigint GENERATED ALWAYS AS IDENTITY UNIQUE, room_id text NOT NULL REFERENCES app.live_rooms(id),
 kind text NOT NULL CHECK(kind IN ('create','close','participant')), target text NOT NULL DEFAULT '',
 state text NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','running','done')), lease_until timestamptz,
 attempts integer NOT NULL DEFAULT 0, created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX live_operation_pending ON app.live_operations(room_id,sequence) WHERE state<>'done';
CREATE TABLE app.live_events (
 id text PRIMARY KEY CHECK(octet_length(id) BETWEEN 1 AND 128), fingerprint text NOT NULL,
 event_type text NOT NULL, room_id text NOT NULL REFERENCES app.live_rooms(id), participant_sid text NOT NULL,
 provider_at timestamptz NOT NULL, received_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TABLE app.live_presence (
 participant_sid text PRIMARY KEY, room_id text NOT NULL REFERENCES app.live_rooms(id),
 account_id text NOT NULL REFERENCES app.identity_accounts(id), association_id text REFERENCES app.agency_host_associations(id),
 classification text NOT NULL, started_at timestamptz, ended_at timestamptz,
 qualifying boolean NOT NULL, valid_until timestamptz,
 CHECK(ended_at IS NULL OR started_at IS NULL OR ended_at>=started_at)
);
CREATE INDEX live_presence_account_window ON app.live_presence(account_id,ended_at) WHERE qualifying AND ended_at IS NOT NULL;
CREATE INDEX live_presence_room_open ON app.live_presence(room_id) WHERE ended_at IS NULL;
CREATE TABLE app.live_commands (
 actor_id text NOT NULL REFERENCES app.identity_accounts(id), kind text NOT NULL, key text NOT NULL,
 fingerprint text NOT NULL, result jsonb NOT NULL, PRIMARY KEY(actor_id,kind,key)
);
-- Completed activity and delivery evidence cannot be rewritten by runtime roles.
-- +goose StatementBegin
CREATE FUNCTION app.live_presence_guard() RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
 IF OLD.ended_at IS NOT NULL THEN RAISE EXCEPTION 'completed presence is immutable'; END IF;
 IF NEW.started_at IS DISTINCT FROM OLD.started_at OR NEW.account_id<>OLD.account_id OR NEW.association_id IS DISTINCT FROM OLD.association_id OR NEW.classification<>OLD.classification OR NEW.qualifying<>OLD.qualifying THEN RAISE EXCEPTION 'presence identity is immutable'; END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER live_presence_integrity BEFORE UPDATE ON app.live_presence FOR EACH ROW EXECUTE FUNCTION app.live_presence_guard();
-- +goose StatementBegin
DO $$ DECLARE r text := current_database() || '_app'; BEGIN
 EXECUTE format('GRANT SELECT,INSERT,UPDATE ON app.agencies,app.host_applications,app.live_rooms,app.live_members,app.live_operations TO %I',r);
 EXECUTE format('GRANT SELECT,INSERT,DELETE ON app.agency_managers TO %I',r);
 EXECUTE format('GRANT SELECT,INSERT ON app.live_events,app.live_commands TO %I',r);
 EXECUTE format('GRANT SELECT,INSERT,UPDATE(ended_at,valid_until) ON app.live_presence TO %I',r);
 EXECUTE format('GRANT USAGE ON SEQUENCE app.live_operations_sequence_seq TO %I',r);
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN IF EXISTS(SELECT 1 FROM app.agencies) OR EXISTS(SELECT 1 FROM app.live_rooms) OR EXISTS(SELECT 1 FROM app.access_role_permissions WHERE permission IN ('agencies.manage','live.rooms.moderate','live.activity.read','live.policies.manage')) THEN RAISE EXCEPTION 'live rollback requires reviewed data recovery'; END IF; END $$;
-- +goose StatementEnd
DROP TABLE app.live_commands;
DROP TABLE app.live_presence;
DROP FUNCTION app.live_presence_guard();
DROP TABLE app.live_events;
DROP TABLE app.live_operations;
DROP TABLE app.live_members;
DROP TABLE app.live_rooms;
DROP TABLE app.host_applications;
DROP TABLE app.agency_managers;
DROP TABLE app.agencies;
ALTER TABLE app.access_role_permissions DROP CONSTRAINT access_role_permissions_permission_check;
ALTER TABLE app.access_role_permissions ADD CONSTRAINT access_role_permissions_permission_check CHECK(permission IN ('users.read','users.private.read','users.profile.update','users.restrict','economy.policies.read','economy.policies.write','wallet.operations.read','wallet.adjust','withdrawals.review','agency.hosts.manage'));
