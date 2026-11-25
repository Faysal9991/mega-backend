-- +goose Up
ALTER TABLE app.access_role_permissions DROP CONSTRAINT access_role_permissions_permission_check;
ALTER TABLE app.access_role_permissions ADD CONSTRAINT access_role_permissions_permission_check CHECK(permission IN ('users.read','users.private.read','users.profile.update','users.restrict','economy.policies.read','economy.policies.write','wallet.operations.read','wallet.adjust','withdrawals.review','agency.hosts.manage','agencies.manage','live.rooms.moderate','live.activity.read','live.policies.manage','gifts.manage','media.manage','notifications.announce','deliveries.read','deliveries.retry'));
CREATE TABLE app.delivery_assets (
 id text PRIMARY KEY CHECK(id ~ '^[0-9a-f]{32}$'), owner_id text NOT NULL REFERENCES app.identity_accounts(id),
 purpose text NOT NULL CHECK(purpose IN ('avatar','agency_logo','gift_thumbnail')),
 target_id text NOT NULL REFERENCES app.identity_accounts(id), object_key text NOT NULL UNIQUE,
 declared_type text NOT NULL CHECK(declared_type IN ('image/png','image/jpeg')), declared_size bigint NOT NULL CHECK(declared_size BETWEEN 1 AND 2097152), checksum text NOT NULL,
 state text NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','processing','ready','failed','retiring','deleted')),
 lease_id text NOT NULL DEFAULT '', lease_until timestamptz,
 approved_key text NOT NULL DEFAULT '', thumbnail_key text NOT NULL DEFAULT '', public_url text NOT NULL DEFAULT '',
 width integer NOT NULL DEFAULT 0 CHECK(width BETWEEN 0 AND 4096), height integer NOT NULL DEFAULT 0 CHECK(height BETWEEN 0 AND 4096),
 size_bytes bigint NOT NULL DEFAULT 0, error_code text NOT NULL DEFAULT '',
 expires_at timestamptz NOT NULL DEFAULT clock_timestamp()+interval '10 minutes',
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(), updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX delivery_asset_owner_page ON app.delivery_assets(owner_id,id) WHERE state<>'deleted';
CREATE TABLE app.delivery_asset_links (
 target_id text NOT NULL REFERENCES app.identity_accounts(id) ON DELETE CASCADE,
 purpose text NOT NULL CHECK(purpose IN ('avatar','agency_logo')), asset_id text NOT NULL REFERENCES app.delivery_assets(id),
 PRIMARY KEY(target_id,purpose)
);
CREATE INDEX delivery_asset_references ON app.delivery_asset_links(asset_id);
CREATE TABLE app.gift_catalog (
 id text PRIMARY KEY, code text NOT NULL UNIQUE CHECK(code ~ '^[a-z][a-z0-9_]{0,31}$'), current_version text, display_order integer NOT NULL DEFAULT 0, active boolean NOT NULL DEFAULT false, created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX gift_catalog_page ON app.gift_catalog(display_order,id);
CREATE INDEX gift_catalog_active_page ON app.gift_catalog(display_order,id) WHERE active;
CREATE TABLE app.gift_versions (
 id text PRIMARY KEY, gift_id text NOT NULL REFERENCES app.gift_catalog(id),
 name text NOT NULL CHECK(octet_length(name) BETWEEN 1 AND 128), description text NOT NULL CHECK(octet_length(description)<=1024),
 price bigint NOT NULL CHECK(price>0), category text NOT NULL CHECK(octet_length(category) BETWEEN 1 AND 64),
 thumbnail_asset_id text REFERENCES app.delivery_assets(id), thumbnail_url text NOT NULL, animation_url text NOT NULL,
 active boolean NOT NULL, display_order integer NOT NULL CHECK(display_order BETWEEN 0 AND 1000000),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
ALTER TABLE app.gift_catalog ADD CONSTRAINT gift_current_version_fk FOREIGN KEY(current_version) REFERENCES app.gift_versions(id);
CREATE INDEX gift_version_history ON app.gift_versions(gift_id,id);
CREATE INDEX gift_asset_reference ON app.gift_versions(thumbnail_asset_id) WHERE thumbnail_asset_id IS NOT NULL;
CREATE TABLE app.gift_recipient_policies (
 id text PRIMARY KEY, previous_id text NOT NULL, mode text NOT NULL CHECK(mode='active_publishers'),
 revision bigint GENERATED ALWAYS AS IDENTITY UNIQUE, created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TABLE app.delivery_outbox (
 id text PRIMARY KEY, topic text NOT NULL CHECK(topic IN ('gift','push','media_gc')), aggregate_id text NOT NULL,
 payload jsonb NOT NULL CHECK(octet_length(payload::text)<=4096),
 state text NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','running','delivered','skipped','failed')),
 attempts integer NOT NULL DEFAULT 0 CHECK(attempts BETWEEN 0 AND 10),
 lease_id text NOT NULL DEFAULT '', lease_until timestamptz,
 available_at timestamptz NOT NULL DEFAULT clock_timestamp(), error_code text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(), updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(topic,aggregate_id)
);
CREATE INDEX delivery_outbox_claim ON app.delivery_outbox(topic,available_at,id) WHERE state IN ('pending','running') AND attempts<10;
CREATE TABLE app.room_gifts (
 operation_id text PRIMARY KEY REFERENCES app.wallet_operations(id), gift_id text NOT NULL REFERENCES app.gift_catalog(id),
 version_id text NOT NULL REFERENCES app.gift_versions(id), room_id text NOT NULL REFERENCES app.live_rooms(id), session_id text NOT NULL,
 sender_id text NOT NULL REFERENCES app.identity_accounts(id), recipient_id text NOT NULL REFERENCES app.identity_accounts(id),
 event jsonb NOT NULL CHECK(octet_length(event::text)<=4096), created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX room_gift_sender_page ON app.room_gifts(sender_id,operation_id);
CREATE INDEX room_gift_recipient_page ON app.room_gifts(recipient_id,operation_id);
CREATE INDEX room_gift_room_page ON app.room_gifts(room_id,operation_id);
CREATE INDEX delivery_outbox_exhausted ON app.delivery_outbox(lease_until,id) WHERE state='running' AND attempts=10;
CREATE TABLE app.push_devices (
 installation_id text PRIMARY KEY REFERENCES app.identity_installations(id) ON DELETE CASCADE,
 account_id text NOT NULL REFERENCES app.identity_accounts(id) ON DELETE CASCADE,
 session_id text NOT NULL REFERENCES app.identity_sessions(id) ON DELETE CASCADE,
 platform text NOT NULL CHECK(platform IN ('android','ios')), token_hash text NOT NULL UNIQUE,
 token_cipher bytea NOT NULL, expires_at timestamptz NOT NULL DEFAULT clock_timestamp()+interval '60 days', revision bigint GENERATED ALWAYS AS IDENTITY UNIQUE,
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX push_device_expiry ON app.push_devices(expires_at,installation_id);
CREATE INDEX push_device_account ON app.push_devices(account_id,installation_id);
CREATE INDEX push_device_session ON app.push_devices(session_id);
CREATE TABLE app.notification_preferences (
 account_id text PRIMARY KEY REFERENCES app.identity_accounts(id) ON DELETE CASCADE,
 gift_received boolean NOT NULL DEFAULT false, agency_decision boolean NOT NULL DEFAULT false,
 account_event boolean NOT NULL DEFAULT false, room_invitation boolean NOT NULL DEFAULT false, admin_announcement boolean NOT NULL DEFAULT false
);
CREATE TABLE app.delivery_announcements (id text PRIMARY KEY, template text NOT NULL CHECK(template IN ('maintenance','service_update','community_update')), created_at timestamptz NOT NULL DEFAULT clock_timestamp());
CREATE TABLE app.notifications (
 id text PRIMARY KEY, account_id text NOT NULL REFERENCES app.identity_accounts(id) ON DELETE CASCADE,
 kind text NOT NULL CHECK(kind IN ('gift_received','agency_decision','account_event','room_invitation','admin_announcement')),
 reference text NOT NULL, created_at timestamptz NOT NULL DEFAULT clock_timestamp(), UNIQUE(account_id,kind,reference)
);
CREATE INDEX notification_account_page ON app.notifications(account_id,id);
CREATE TABLE app.delivery_commands (
 actor_id text NOT NULL REFERENCES app.identity_accounts(id), kind text NOT NULL, key text NOT NULL,
 fingerprint text NOT NULL, result jsonb NOT NULL, PRIMARY KEY(actor_id,kind,key)
);
-- +goose StatementBegin
DO $$ DECLARE r text := current_database()||'_app'; BEGIN
 EXECUTE format('GRANT SELECT,INSERT ON app.delivery_assets,app.gift_catalog,app.delivery_outbox TO %I',r);
 EXECUTE format('GRANT UPDATE(state,lease_id,lease_until,approved_key,thumbnail_key,public_url,width,height,size_bytes,error_code,updated_at) ON app.delivery_assets TO %I',r);
 EXECUTE format('GRANT UPDATE(current_version,display_order,active) ON app.gift_catalog TO %I',r);
 EXECUTE format('GRANT UPDATE(state,attempts,lease_id,lease_until,available_at,error_code,updated_at) ON app.delivery_outbox TO %I',r);
 EXECUTE format('GRANT SELECT,INSERT,UPDATE ON app.notification_preferences TO %I',r);
 EXECUTE format('GRANT SELECT,INSERT,UPDATE,DELETE ON app.delivery_asset_links,app.push_devices TO %I',r);
 EXECUTE format('GRANT SELECT,INSERT ON app.gift_versions,app.gift_recipient_policies,app.room_gifts,app.notifications,app.delivery_announcements,app.delivery_commands TO %I',r);
 EXECUTE format('GRANT USAGE ON SEQUENCE app.gift_recipient_policies_revision_seq,app.push_devices_revision_seq TO %I',r);
 EXECUTE format('GRANT UPDATE(avatar_url) ON app.profiles TO %I',r);
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM app.delivery_assets) OR EXISTS(SELECT 1 FROM app.gift_catalog) OR EXISTS(SELECT 1 FROM app.delivery_outbox) OR EXISTS(SELECT 1 FROM app.gift_recipient_policies) OR EXISTS(SELECT 1 FROM app.push_devices) OR EXISTS(SELECT 1 FROM app.notifications) OR EXISTS(SELECT 1 FROM app.delivery_announcements) OR EXISTS(SELECT 1 FROM app.delivery_commands) OR EXISTS(SELECT 1 FROM app.access_role_permissions WHERE permission IN ('gifts.manage','media.manage','notifications.announce','deliveries.read','deliveries.retry')) THEN RAISE EXCEPTION 'delivery rollback requires reviewed retained-data recovery'; END IF;
END $$;
-- +goose StatementEnd
DROP TABLE app.delivery_commands;
DROP TABLE app.notifications;
DROP TABLE app.delivery_announcements;
DROP TABLE app.notification_preferences;
DROP TABLE app.push_devices;
DROP TABLE app.room_gifts;
DROP TABLE app.delivery_outbox;
DROP TABLE app.gift_recipient_policies;
ALTER TABLE app.gift_catalog DROP CONSTRAINT gift_current_version_fk;
DROP TABLE app.gift_versions;
DROP TABLE app.gift_catalog;
DROP TABLE app.delivery_asset_links;
DROP TABLE app.delivery_assets;
-- Existing profile update grants are retained; only the module-owned adapter uses them.
ALTER TABLE app.access_role_permissions DROP CONSTRAINT access_role_permissions_permission_check;
ALTER TABLE app.access_role_permissions ADD CONSTRAINT access_role_permissions_permission_check CHECK(permission IN ('users.read','users.private.read','users.profile.update','users.restrict','economy.policies.read','economy.policies.write','wallet.operations.read','wallet.adjust','withdrawals.review','agency.hosts.manage','agencies.manage','live.rooms.moderate','live.activity.read','live.policies.manage'));
