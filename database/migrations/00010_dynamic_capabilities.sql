-- +goose Up
-- +goose StatementBegin
CREATE TABLE app.access_capabilities(action text PRIMARY KEY, package_safe boolean NOT NULL);
INSERT INTO app.access_capabilities(action,package_safe) VALUES
('users.read',false),
('users.private.read',false),
('users.profile.update',false),
('users.restrict',false),
('economy.policies.read',false),
('economy.policies.write',false),
('wallet.operations.read',false),
('wallet.adjust',false),
('withdrawals.review',false),
('agency.hosts.manage',false),
('agencies.manage',false),
('live.rooms.moderate',false),
('live.activity.read',false),
('live.policies.manage',false),
('gifts.manage',false),
('media.manage',false),
('notifications.announce',false),
('deliveries.read',false),
('deliveries.retry',false),
('capabilities.manage',false),
('packages.manage',false),
('packages.entitlements.manage',false),
('packages.purchases.read',false),
('room.enter',true),
('room.enter.private',false),
('room.join.hidden',false),
('room.close',false),
('room.participant.kick',false),
('room.participant.mute',false),
('room.participant.seat_remove',false),
('room.participant.ban',false),
('live.participant.global_ban',false),
('room.seat.lock',false),
('room.seat.unlock',false),
('room.seat.self_join',true),
('room.seat.self_leave',true),
('room.title.update',false),
('room.announcement.update',false),
('room.admin.assign',false),
('room.moderator.assign',false),
('room.settings.update',false),
('room.moderation_history.read',false),
('benefit.badge',true),
('benefit.entrance',true),
('packages.purchase',false);
ALTER TABLE app.access_role_permissions DROP CONSTRAINT access_role_permissions_permission_check;
ALTER TABLE app.access_role_permissions ADD CONSTRAINT access_role_capability_fk FOREIGN KEY(permission) REFERENCES app.access_capabilities(action);
CREATE TABLE app.access_policy_versions(id text PRIMARY KEY, previous_id text REFERENCES app.access_policy_versions(id), roles jsonb NOT NULL CHECK(octet_length(roles::text)<=16384), created_at timestamptz NOT NULL DEFAULT clock_timestamp());
INSERT INTO app.access_policy_versions(id,roles) VALUES('00000000000000000000000000000001','{"host": ["room.close", "room.participant.kick", "room.participant.mute", "room.participant.seat_remove", "room.participant.ban", "room.seat.lock", "room.seat.unlock", "room.seat.self_join", "room.seat.self_leave", "room.title.update", "room.announcement.update", "room.admin.assign", "room.moderator.assign", "room.settings.update", "room.moderation_history.read"], "co_host": ["room.participant.kick", "room.participant.mute", "room.participant.seat_remove", "room.participant.ban", "room.seat.self_leave"], "audience": ["room.seat.self_leave"], "admin": ["room.close", "room.participant.kick", "room.participant.mute", "room.participant.seat_remove", "room.participant.ban", "room.seat.lock", "room.seat.unlock", "room.seat.self_join", "room.seat.self_leave", "room.title.update", "room.announcement.update", "room.admin.assign", "room.moderator.assign", "room.settings.update", "room.moderation_history.read"], "moderator": ["room.participant.kick", "room.participant.mute", "room.participant.seat_remove", "room.participant.ban", "room.seat.self_leave"]}');
CREATE TABLE app.access_policy_head(singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton), policy_id text NOT NULL REFERENCES app.access_policy_versions(id), revision bigint NOT NULL DEFAULT 1);
INSERT INTO app.access_policy_head(singleton,policy_id) VALUES(true,'00000000000000000000000000000001');
CREATE TABLE app.access_overrides(account_id text NOT NULL REFERENCES app.identity_accounts(id), room_id text NOT NULL DEFAULT '', action text NOT NULL REFERENCES app.access_capabilities(action), allowed boolean NOT NULL, expires_at timestamptz NOT NULL, PRIMARY KEY(account_id,room_id,action));
CREATE INDEX access_override_actor_entry_expiry ON app.access_overrides(account_id,expires_at) WHERE action='room.enter';
CREATE INDEX access_override_expiry ON app.access_overrides(expires_at);
CREATE INDEX access_audit_target_page ON app.access_audit(target,id);
CREATE TABLE app.live_global_bans(account_id text PRIMARY KEY REFERENCES app.identity_accounts(id), active boolean NOT NULL, updated_at timestamptz NOT NULL DEFAULT clock_timestamp());
ALTER TABLE app.live_rooms ADD COLUMN visibility text NOT NULL DEFAULT 'public' CHECK(visibility IN ('public','private','hidden')), ADD COLUMN announcement text NOT NULL DEFAULT '' CHECK(octet_length(announcement)<=1024), ADD COLUMN seats_locked boolean NOT NULL DEFAULT false;
ALTER TABLE app.live_members ADD COLUMN hidden boolean NOT NULL DEFAULT false, ADD COLUMN self_seated boolean NOT NULL DEFAULT false, ADD COLUMN private_entry boolean NOT NULL DEFAULT false;
ALTER TABLE app.live_members DROP CONSTRAINT live_members_role_check;
ALTER TABLE app.live_members ADD CONSTRAINT live_members_role_check CHECK(role IN ('host','admin','co_host','moderator','audience'));
DO $$ DECLARE r text:=current_database()||'_app'; BEGIN
 EXECUTE format('GRANT SELECT ON app.access_capabilities,app.access_policy_versions,app.access_policy_head TO %I',r);
 EXECUTE format('GRANT INSERT ON app.access_policy_versions TO %I',r);
 EXECUTE format('GRANT UPDATE(policy_id,revision) ON app.access_policy_head TO %I',r);
 EXECUTE format('GRANT SELECT,INSERT,UPDATE,DELETE ON app.access_overrides TO %I',r);
 EXECUTE format('GRANT SELECT,INSERT,UPDATE ON app.live_global_bans TO %I',r);
 EXECUTE format('GRANT UPDATE(visibility,announcement,seats_locked,title) ON app.live_rooms TO %I',r);
 EXECUTE format('GRANT UPDATE(hidden,self_seated,private_entry) ON app.live_members TO %I',r);
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
 IF (SELECT count(*) FROM app.access_policy_versions)>1 OR EXISTS(SELECT 1 FROM app.access_overrides) OR EXISTS(SELECT 1 FROM app.live_global_bans) OR EXISTS(SELECT 1 FROM app.live_members m WHERE hidden OR self_seated OR role='admin' OR COALESCE((to_jsonb(m)->>'private_entry')::boolean,false)) OR EXISTS(SELECT 1 FROM app.live_rooms WHERE visibility<>'public' OR announcement<>'' OR seats_locked) OR EXISTS(SELECT 1 FROM app.access_role_permissions WHERE permission NOT IN ('users.read','users.private.read','users.profile.update','users.restrict','economy.policies.read','economy.policies.write','wallet.operations.read','wallet.adjust','withdrawals.review','agency.hosts.manage','agencies.manage','live.rooms.moderate','live.activity.read','live.policies.manage','gifts.manage','media.manage','notifications.announce','deliveries.read','deliveries.retry')) THEN RAISE EXCEPTION 'capability rollback requires reviewed retained-data recovery'; END IF;
END $$;
ALTER TABLE app.live_members DROP CONSTRAINT live_members_role_check;
ALTER TABLE app.live_members ADD CONSTRAINT live_members_role_check CHECK(role IN ('host','co_host','moderator','audience'));
ALTER TABLE app.live_members DROP COLUMN hidden,DROP COLUMN self_seated,DROP COLUMN IF EXISTS private_entry;
ALTER TABLE app.live_rooms DROP COLUMN visibility,DROP COLUMN announcement,DROP COLUMN seats_locked;
DROP TABLE app.live_global_bans;
DROP INDEX IF EXISTS app.access_audit_target_page;
DROP TABLE app.access_overrides;
DROP TABLE app.access_policy_head;
DROP TABLE app.access_policy_versions;
ALTER TABLE app.access_role_permissions DROP CONSTRAINT access_role_capability_fk;
DROP TABLE app.access_capabilities;
ALTER TABLE app.access_role_permissions ADD CONSTRAINT access_role_permissions_permission_check CHECK(permission IN ('users.read','users.private.read','users.profile.update','users.restrict','economy.policies.read','economy.policies.write','wallet.operations.read','wallet.adjust','withdrawals.review','agency.hosts.manage','agencies.manage','live.rooms.moderate','live.activity.read','live.policies.manage','gifts.manage','media.manage','notifications.announce','deliveries.read','deliveries.retry'));
-- +goose StatementEnd
