-- +goose Up
-- +goose StatementBegin
ALTER TABLE app.live_operations ADD COLUMN lease_token text NOT NULL DEFAULT '' CHECK(lease_token='' OR lease_token ~ '^[0-9a-f]{32}$');
CREATE TABLE app.package_catalog(id text PRIMARY KEY,code text NOT NULL UNIQUE CHECK(code ~ '^[a-z][a-z0-9_]{0,31}$'),current_version text,created_at timestamptz NOT NULL DEFAULT clock_timestamp());
CREATE TABLE app.package_versions(id text PRIMARY KEY,package_id text NOT NULL REFERENCES app.package_catalog(id),name text NOT NULL CHECK(octet_length(name) BETWEEN 1 AND 128),description text NOT NULL CHECK(octet_length(description)<=1024),price bigint NOT NULL CHECK(price>0),duration_seconds bigint NOT NULL CHECK(duration_seconds BETWEEN 60 AND 31536000),powers text[] NOT NULL CHECK(cardinality(powers)<=5),badge_asset_id text REFERENCES app.delivery_assets(id),entrance_asset_id text REFERENCES app.delivery_assets(id),listing_priority integer NOT NULL CHECK(listing_priority BETWEEN 0 AND 1000000),active boolean NOT NULL,created_at timestamptz NOT NULL DEFAULT clock_timestamp());
CREATE UNIQUE INDEX package_version_history ON app.package_versions(package_id,id);
ALTER TABLE app.package_catalog ADD CONSTRAINT package_current_version_fk FOREIGN KEY(id,current_version) REFERENCES app.package_versions(package_id,id);
CREATE INDEX package_badge_reference ON app.package_versions(badge_asset_id) WHERE badge_asset_id IS NOT NULL;
CREATE INDEX package_entrance_reference ON app.package_versions(entrance_asset_id) WHERE entrance_asset_id IS NOT NULL;
CREATE TABLE app.package_entitlements(id text PRIMARY KEY,account_id text NOT NULL REFERENCES app.identity_accounts(id),package_id text NOT NULL REFERENCES app.package_catalog(id),starts_at timestamptz NOT NULL,expires_at timestamptz NOT NULL CHECK(expires_at>starts_at),source text NOT NULL CHECK(source IN ('purchase','direct')),created_at timestamptz NOT NULL DEFAULT clock_timestamp());
CREATE INDEX package_entitlement_account ON app.package_entitlements(account_id,expires_at,package_id) INCLUDE(starts_at,id);
CREATE INDEX package_entitlement_subscribers ON app.package_entitlements(package_id,id) INCLUDE(account_id,starts_at,expires_at);
CREATE INDEX package_entitlement_expiry ON app.package_entitlements(expires_at);
CREATE TABLE app.package_revocations(entitlement_id text PRIMARY KEY REFERENCES app.package_entitlements(id),actor_id text NOT NULL REFERENCES app.identity_accounts(id),created_at timestamptz NOT NULL DEFAULT clock_timestamp());
CREATE TABLE app.package_commands(actor_id text NOT NULL REFERENCES app.identity_accounts(id),kind text NOT NULL CHECK(kind IN ('save','grant','revoke')),key text NOT NULL CHECK(octet_length(key) BETWEEN 1 AND 64),fingerprint text NOT NULL CHECK(fingerprint ~ '^[0-9a-f]{64}$'),result jsonb NOT NULL CHECK(octet_length(result::text)<=16384),PRIMARY KEY(actor_id,kind,key));
CREATE TABLE app.package_purchases(operation_id text PRIMARY KEY REFERENCES app.wallet_operations(id),account_id text NOT NULL REFERENCES app.identity_accounts(id),package_id text NOT NULL REFERENCES app.package_catalog(id),version_id text NOT NULL REFERENCES app.package_versions(id),entitlement_id text NOT NULL UNIQUE REFERENCES app.package_entitlements(id),price bigint NOT NULL CHECK(price>0),created_at timestamptz NOT NULL DEFAULT clock_timestamp());
CREATE INDEX package_purchase_account ON app.package_purchases(account_id,operation_id);
CREATE INDEX package_purchase_catalog ON app.package_purchases(package_id,operation_id);
-- Owner-confirmed: 100% of diamond price is platform profit; no VAT/commission.
CREATE TABLE app.package_financial_policies(id text PRIMARY KEY,mode text NOT NULL CHECK(mode='platform_full_no_vat'),created_at timestamptz NOT NULL DEFAULT clock_timestamp());
INSERT INTO app.package_financial_policies(id,mode) VALUES('00000000000000000000000000000002','platform_full_no_vat');
CREATE TABLE app.package_profit(operation_id text PRIMARY KEY REFERENCES app.package_purchases(operation_id),diamonds bigint NOT NULL CHECK(diamonds>0),policy_id text NOT NULL REFERENCES app.package_financial_policies(id),created_at timestamptz NOT NULL DEFAULT clock_timestamp());
ALTER TABLE app.wallet_accounts DROP CONSTRAINT wallet_accounts_check;
ALTER TABLE app.wallet_accounts ADD CONSTRAINT wallet_accounts_identity_check CHECK((kind IN ('user','held') AND owner_id IS NOT NULL AND id=kind||':'||owner_id) OR (owner_id IS NULL AND ((id IN ('platform:fees','platform:gifts','platform:packages') AND kind='platform') OR (id='platform:tax' AND kind='tax') OR (id='platform:issuance' AND kind='issuance'))));
INSERT INTO app.wallet_accounts(id,kind) VALUES('platform:packages','platform');
ALTER TABLE app.wallet_operations DROP CONSTRAINT wallet_operations_kind_check;
ALTER TABLE app.wallet_operations ADD CONSTRAINT wallet_operations_kind_check CHECK(kind IN ('transfer','gift','adjustment','withdrawal_reserve','withdrawal_release','package_purchase'));
ALTER FUNCTION app.wallet_post(text,text[],bigint[]) RENAME TO wallet_post_existing;
REVOKE ALL ON FUNCTION app.wallet_post_existing(text,text[],bigint[]) FROM PUBLIC;
CREATE OR REPLACE FUNCTION app.wallet_post(op text,ids text[],amounts bigint[]) RETURNS void LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE o app.wallet_operations%ROWTYPE; p app.package_purchases%ROWTYPE; b bigint; k text; i integer;
BEGIN
 SELECT * INTO STRICT o FROM app.wallet_operations WHERE id=op;
 IF o.kind<>'package_purchase' THEN PERFORM app.wallet_post_existing(op,ids,amounts); RETURN; END IF;
 SELECT * INTO STRICT p FROM app.package_purchases WHERE operation_id=op;
 IF o.actor_id<>o.target_id OR o.actor_id<>p.account_id OR o.amount<>p.price OR o.fee<>0 OR o.policy_id IS NOT NULL OR o.reference<>p.entitlement_id OR o.policy_snapshot<>jsonb_build_object('accounting_policy','platform_full_no_vat','package_version_id',p.version_id) OR NOT EXISTS(SELECT 1 FROM app.package_financial_policies WHERE id='00000000000000000000000000000002' AND mode='platform_full_no_vat') OR NOT EXISTS(SELECT 1 FROM app.package_versions v JOIN app.package_entitlements e ON e.id=p.entitlement_id WHERE v.id=p.version_id AND v.package_id=p.package_id AND v.price=p.price AND v.active AND EXISTS(SELECT 1 FROM app.package_catalog c WHERE c.id=v.package_id AND c.current_version=v.id) AND e.account_id=p.account_id AND e.package_id=p.package_id AND e.source='purchase' AND e.starts_at>=o.created_at AND e.expires_at=e.starts_at+v.duration_seconds*interval '1 second') THEN RAISE EXCEPTION 'invalid package accounting snapshot'; END IF;
 IF ids IS DISTINCT FROM ARRAY['platform:packages','user:'||o.actor_id] OR amounts IS DISTINCT FROM ARRAY[o.amount,-o.amount] OR EXISTS(SELECT 1 FROM app.wallet_entries WHERE operation_id=op) THEN RAISE EXCEPTION 'invalid package postings'; END IF;
 PERFORM id FROM app.wallet_accounts WHERE id=ANY(ids) ORDER BY id FOR UPDATE;
 IF (SELECT starts_at FROM app.package_entitlements WHERE id=p.entitlement_id) IS DISTINCT FROM (SELECT GREATEST(o.created_at,COALESCE(max(e.expires_at),o.created_at)) FROM app.package_entitlements e WHERE e.account_id=p.account_id AND e.package_id=p.package_id AND e.id<>p.entitlement_id AND e.expires_at>o.created_at AND NOT EXISTS(SELECT 1 FROM app.package_revocations r WHERE r.entitlement_id=e.id)) THEN RAISE EXCEPTION 'package period must append after existing terms'; END IF;

 FOR i IN 1..2 LOOP
  SELECT balance,kind INTO STRICT b,k FROM app.wallet_accounts WHERE id=ids[i];
  IF b::numeric+amounts[i]::numeric<0 OR b::numeric+amounts[i]::numeric>9223372036854775807 THEN RAISE EXCEPTION USING ERRCODE='P0002',MESSAGE='wallet range or funds violation'; END IF;
  UPDATE app.wallet_accounts SET balance=balance+amounts[i] WHERE id=ids[i];
  INSERT INTO app.wallet_entries(operation_id,account_id,delta) VALUES(op,ids[i],amounts[i]);
 END LOOP;
 INSERT INTO app.package_profit(operation_id,diamonds,policy_id) VALUES(op,o.amount,'00000000000000000000000000000002');
END $$;
REVOKE ALL ON FUNCTION app.wallet_post(text,text[],bigint[]) FROM PUBLIC;
CREATE FUNCTION app.capability_changed() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$ BEGIN UPDATE app.access_policy_head SET revision=revision+1 WHERE singleton;RETURN NULL;END $$;
REVOKE ALL ON FUNCTION app.capability_changed() FROM PUBLIC;
CREATE TRIGGER capability_packages AFTER INSERT OR UPDATE OR DELETE ON app.package_catalog FOR EACH STATEMENT EXECUTE FUNCTION app.capability_changed();
CREATE TRIGGER capability_entitlements AFTER INSERT OR DELETE ON app.package_entitlements FOR EACH STATEMENT EXECUTE FUNCTION app.capability_changed();
CREATE TRIGGER capability_revocations AFTER INSERT OR DELETE ON app.package_revocations FOR EACH STATEMENT EXECUTE FUNCTION app.capability_changed();
CREATE TRIGGER capability_roles AFTER INSERT OR UPDATE OR DELETE ON app.access_role_permissions FOR EACH STATEMENT EXECUTE FUNCTION app.capability_changed();
CREATE TRIGGER capability_assignments AFTER INSERT OR DELETE ON app.access_assignments FOR EACH STATEMENT EXECUTE FUNCTION app.capability_changed();
CREATE TABLE app.live_discovery_head(singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton),revision bigint NOT NULL DEFAULT 1);
INSERT INTO app.live_discovery_head(singleton) VALUES(true);
CREATE FUNCTION app.discovery_changed() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$ BEGIN UPDATE app.live_discovery_head SET revision=revision+1 WHERE singleton; RETURN NULL; END $$;
REVOKE ALL ON FUNCTION app.discovery_changed() FROM PUBLIC;
CREATE TRIGGER discovery_packages AFTER INSERT OR UPDATE OR DELETE ON app.package_catalog FOR EACH STATEMENT EXECUTE FUNCTION app.discovery_changed();
CREATE FUNCTION app.discovery_entitlement_changed() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$ DECLARE actor text; BEGIN
 IF TG_TABLE_NAME='package_revocations' THEN SELECT account_id INTO actor FROM app.package_entitlements WHERE id=CASE WHEN TG_OP='DELETE' THEN OLD.entitlement_id ELSE NEW.entitlement_id END;
 ELSE actor:=CASE WHEN TG_OP='DELETE' THEN OLD.account_id ELSE NEW.account_id END; END IF;
 IF EXISTS(SELECT 1 FROM app.live_rooms WHERE owner_id=actor AND state='open' AND visibility='public') THEN UPDATE app.live_discovery_head SET revision=revision+1 WHERE singleton;END IF;RETURN NULL;END $$;
REVOKE ALL ON FUNCTION app.discovery_entitlement_changed() FROM PUBLIC;
CREATE TRIGGER discovery_entitlements AFTER INSERT OR DELETE ON app.package_entitlements FOR EACH ROW EXECUTE FUNCTION app.discovery_entitlement_changed();
CREATE TRIGGER discovery_revocations AFTER INSERT OR DELETE ON app.package_revocations FOR EACH ROW EXECUTE FUNCTION app.discovery_entitlement_changed();
CREATE TRIGGER discovery_rooms AFTER INSERT OR DELETE ON app.live_rooms FOR EACH STATEMENT EXECUTE FUNCTION app.discovery_changed();
CREATE TRIGGER discovery_room_state AFTER UPDATE OF state,visibility,owner_id ON app.live_rooms FOR EACH ROW WHEN(OLD.state IS DISTINCT FROM NEW.state OR OLD.visibility IS DISTINCT FROM NEW.visibility OR OLD.owner_id IS DISTINCT FROM NEW.owner_id) EXECUTE FUNCTION app.discovery_changed();
CREATE TRIGGER discovery_accounts AFTER UPDATE OF status ON app.identity_accounts FOR EACH ROW WHEN(OLD.status IS DISTINCT FROM NEW.status) EXECUTE FUNCTION app.discovery_changed();
CREATE TRIGGER discovery_bans AFTER INSERT OR UPDATE OR DELETE ON app.live_global_bans FOR EACH STATEMENT EXECUTE FUNCTION app.discovery_changed();
CREATE TRIGGER discovery_members AFTER UPDATE OF state,role ON app.live_members FOR EACH ROW WHEN(((OLD.role='host' OR NEW.role='host') AND (OLD.state IS DISTINCT FROM NEW.state OR OLD.role IS DISTINCT FROM NEW.role)) OR (OLD.state IN ('banned','kicked')) IS DISTINCT FROM (NEW.state IN ('banned','kicked'))) EXECUTE FUNCTION app.discovery_changed();
CREATE TRIGGER discovery_member_deleted AFTER DELETE ON app.live_members FOR EACH ROW WHEN(OLD.role='host' OR OLD.state IN ('banned','kicked')) EXECUTE FUNCTION app.discovery_changed();
CREATE TRIGGER discovery_member_inserted AFTER INSERT ON app.live_members FOR EACH ROW WHEN(NEW.role='host' OR NEW.state IN ('banned','kicked')) EXECUTE FUNCTION app.discovery_changed();
CREATE TABLE app.live_list_snapshots(id text PRIMARY KEY,actor_id text NOT NULL REFERENCES app.identity_accounts(id) ON DELETE CASCADE,revision bigint NOT NULL,expires_at timestamptz NOT NULL);
CREATE INDEX live_list_snapshot_expiry ON app.live_list_snapshots(expires_at,id);
CREATE INDEX live_list_snapshot_actor ON app.live_list_snapshots(actor_id,expires_at);
CREATE INDEX package_entitlement_start ON app.package_entitlements(starts_at);
CREATE INDEX live_dispatch_oldest ON app.live_operations(sequence,room_id) WHERE state<>'done' AND attempts<100;
CREATE INDEX live_capability_members ON app.live_members(room_id,account_id) WHERE state='active' AND (hidden OR self_seated OR private_entry);
CREATE TRIGGER discovery_overrides AFTER INSERT OR UPDATE OR DELETE ON app.access_overrides FOR EACH STATEMENT EXECUTE FUNCTION app.discovery_changed();
CREATE INDEX live_discovery_available ON app.live_rooms(owner_id,id) WHERE state='open' AND visibility='public';
DO $$ DECLARE r text:=current_database()||'_app'; BEGIN
 EXECUTE format('REVOKE ALL ON FUNCTION app.wallet_post_existing(text,text[],bigint[]) FROM %I',r);
 EXECUTE format('GRANT EXECUTE ON FUNCTION app.wallet_post(text,text[],bigint[]) TO %I',r);
 EXECUTE format('GRANT SELECT,INSERT,DELETE ON app.live_list_snapshots TO %I',r);
 EXECUTE format('GRANT SELECT ON app.package_profit,app.package_financial_policies,app.live_discovery_head TO %I',r);
 EXECUTE format('GRANT SELECT,INSERT ON app.package_catalog,app.package_versions,app.package_entitlements,app.package_revocations,app.package_commands,app.package_purchases TO %I',r);
 EXECUTE format('GRANT UPDATE(current_version) ON app.package_catalog TO %I',r);
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN IF EXISTS(SELECT 1 FROM app.live_operations WHERE state='running') OR EXISTS(SELECT 1 FROM app.package_catalog) OR EXISTS(SELECT 1 FROM app.package_entitlements) OR EXISTS(SELECT 1 FROM app.package_purchases) OR EXISTS(SELECT 1 FROM app.package_profit) OR EXISTS(SELECT 1 FROM app.package_commands) OR EXISTS(SELECT 1 FROM app.wallet_accounts WHERE id='platform:packages' AND balance<>0) THEN RAISE EXCEPTION 'package rollback requires reviewed retained-data recovery'; END IF; END $$;
DROP TRIGGER IF EXISTS capability_assignments ON app.access_assignments;
DROP TRIGGER IF EXISTS capability_roles ON app.access_role_permissions;
DROP TRIGGER IF EXISTS capability_revocations ON app.package_revocations;
DROP TRIGGER IF EXISTS capability_entitlements ON app.package_entitlements;
DROP TRIGGER IF EXISTS capability_packages ON app.package_catalog;
DROP FUNCTION IF EXISTS app.capability_changed();
ALTER TABLE app.live_operations DROP COLUMN IF EXISTS lease_token;
DROP TABLE app.live_list_snapshots;
DROP INDEX IF EXISTS app.live_dispatch_oldest;
DROP INDEX app.live_capability_members;
DROP INDEX app.live_discovery_available;
DROP TRIGGER discovery_overrides ON app.access_overrides;
DROP TRIGGER IF EXISTS discovery_member_inserted ON app.live_members;
DROP TRIGGER IF EXISTS discovery_member_deleted ON app.live_members;
DROP TRIGGER discovery_members ON app.live_members;
DROP TRIGGER discovery_bans ON app.live_global_bans;
DROP TRIGGER discovery_accounts ON app.identity_accounts;
DROP TRIGGER IF EXISTS discovery_room_state ON app.live_rooms;
DROP TRIGGER discovery_rooms ON app.live_rooms;
DROP TRIGGER discovery_revocations ON app.package_revocations;
DROP TRIGGER discovery_entitlements ON app.package_entitlements;
DROP FUNCTION IF EXISTS app.discovery_entitlement_changed();
DROP TRIGGER discovery_packages ON app.package_catalog;
DROP FUNCTION app.discovery_changed();
DROP TABLE app.live_discovery_head;
DROP FUNCTION app.wallet_post(text,text[],bigint[]);
ALTER FUNCTION app.wallet_post_existing(text,text[],bigint[]) RENAME TO wallet_post;
DO $$ BEGIN EXECUTE format('GRANT EXECUTE ON FUNCTION app.wallet_post(text,text[],bigint[]) TO %I',current_database()||'_app'); END $$;
DELETE FROM app.wallet_accounts WHERE id='platform:packages';
ALTER TABLE app.wallet_accounts DROP CONSTRAINT wallet_accounts_identity_check;
ALTER TABLE app.wallet_accounts ADD CONSTRAINT wallet_accounts_check CHECK((kind IN ('user','held') AND owner_id IS NOT NULL AND id=kind||':'||owner_id) OR (owner_id IS NULL AND ((id IN ('platform:fees','platform:gifts') AND kind='platform') OR (id='platform:tax' AND kind='tax') OR (id='platform:issuance' AND kind='issuance'))));
ALTER TABLE app.wallet_operations DROP CONSTRAINT wallet_operations_kind_check;
ALTER TABLE app.wallet_operations ADD CONSTRAINT wallet_operations_kind_check CHECK(kind IN ('transfer','gift','adjustment','withdrawal_reserve','withdrawal_release'));
DROP TABLE app.package_profit;
DROP TABLE app.package_financial_policies;
DROP TABLE app.package_purchases;
DROP TABLE app.package_commands;
DROP TABLE app.package_revocations;
DROP TABLE app.package_entitlements;
ALTER TABLE app.package_catalog DROP CONSTRAINT package_current_version_fk;
DROP TABLE app.package_versions;
DROP TABLE app.package_catalog;
-- +goose StatementEnd
