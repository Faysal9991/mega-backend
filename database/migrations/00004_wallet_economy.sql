-- +goose Up
ALTER TABLE app.access_role_permissions DROP CONSTRAINT access_role_permissions_permission_check;
ALTER TABLE app.access_role_permissions ADD CONSTRAINT access_role_permissions_permission_check CHECK(permission IN ('users.read','users.private.read','users.profile.update','users.restrict','economy.policies.read','economy.policies.write','wallet.operations.read','wallet.adjust','withdrawals.review','agency.hosts.manage'));
CREATE TABLE app.wallet_accounts (
 id text PRIMARY KEY,
 owner_id text REFERENCES app.identity_accounts(id) ON DELETE CASCADE,
 kind text NOT NULL CHECK(kind IN ('user','held','platform','tax','issuance')),
 balance bigint NOT NULL DEFAULT 0,
 CHECK((kind IN ('user','held') AND owner_id IS NOT NULL AND id=kind||':'||owner_id) OR (owner_id IS NULL AND ((id IN ('platform:fees','platform:gifts') AND kind='platform') OR (id='platform:tax' AND kind='tax') OR (id='platform:issuance' AND kind='issuance')))),
 CHECK((kind='issuance' AND balance<=0 AND balance>=-9223372036854775807) OR (kind<>'issuance' AND balance>=0)),
 UNIQUE(owner_id,kind)
);
INSERT INTO app.wallet_accounts(id,kind) VALUES('platform:fees','platform'),('platform:gifts','platform'),('platform:tax','tax'),('platform:issuance','issuance');
INSERT INTO app.wallet_accounts(id,owner_id,kind) SELECT 'user:'||id,id,'user' FROM app.identity_accounts;
INSERT INTO app.wallet_accounts(id,owner_id,kind) SELECT 'held:'||id,id,'held' FROM app.identity_accounts;
CREATE TABLE app.economy_policies (
 revision bigint GENERATED ALWAYS AS IDENTITY UNIQUE,
 id text PRIMARY KEY CHECK(id ~ '^[0-9a-f]{32}$'),
 kind text NOT NULL CHECK(kind IN ('transfer','withdrawal','gift','activity')),
 scope text NOT NULL CHECK(scope IN ('all','agency','non_agency','audio_host','video_host')),
 previous_id text REFERENCES app.economy_policies(id),
 effective_at timestamptz NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 actor_id text NOT NULL REFERENCES app.identity_accounts(id),
 config jsonb NOT NULL CHECK(jsonb_typeof(config)='object' AND octet_length(config::text)<=16384)
);
CREATE INDEX economy_policy_effective ON app.economy_policies(kind,scope,effective_at DESC,revision DESC);
CREATE INDEX economy_policy_publication ON app.economy_policies(kind,scope,revision DESC);
CREATE TABLE app.economy_commands (
 actor_id text NOT NULL REFERENCES app.identity_accounts(id),kind text NOT NULL,idempotency_key text NOT NULL CHECK(octet_length(idempotency_key) BETWEEN 1 AND 64),
 fingerprint text NOT NULL CHECK(fingerprint ~ '^[0-9a-f]{64}$'),result jsonb NOT NULL CHECK(octet_length(result::text)<=32768),
 PRIMARY KEY(actor_id,kind,idempotency_key)
);
CREATE TABLE app.wallet_operations (
 id text PRIMARY KEY CHECK(id ~ '^[0-9a-f]{32}$'),
 kind text NOT NULL CHECK(kind IN ('transfer','gift','adjustment','withdrawal_reserve','withdrawal_release')),
 actor_id text NOT NULL REFERENCES app.identity_accounts(id),
 target_id text NOT NULL REFERENCES app.identity_accounts(id),
 idempotency_key text NOT NULL CHECK(octet_length(idempotency_key) BETWEEN 1 AND 64),
 fingerprint text NOT NULL CHECK(fingerprint ~ '^[0-9a-f]{64}$'),
 amount bigint NOT NULL CHECK(amount>0),
 fee bigint NOT NULL CHECK(fee>=0),
 policy_id text REFERENCES app.economy_policies(id),
 policy_snapshot jsonb NOT NULL DEFAULT '{}'::jsonb CHECK(octet_length(policy_snapshot::text)<=16384),
 reference text NOT NULL CHECK(octet_length(reference)<=64),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(actor_id,kind,idempotency_key)
);
CREATE UNIQUE INDEX wallet_single_withdrawal_release ON app.wallet_operations(reference) WHERE kind='withdrawal_release';
CREATE INDEX wallet_operations_limits ON app.wallet_operations(actor_id,kind,created_at) INCLUDE(amount);
CREATE TABLE app.wallet_entries (
 operation_id text NOT NULL REFERENCES app.wallet_operations(id),
 account_id text NOT NULL REFERENCES app.wallet_accounts(id),
 delta bigint NOT NULL CHECK(delta<>0),
 PRIMARY KEY(operation_id,account_id)
);
CREATE INDEX wallet_entries_account_operation ON app.wallet_entries(account_id,operation_id);
CREATE TABLE app.agency_host_associations (
 id text PRIMARY KEY CHECK(id ~ '^[0-9a-f]{32}$'),
 account_id text NOT NULL REFERENCES app.identity_accounts(id),
 agency_id text NOT NULL REFERENCES app.identity_accounts(id),
 classification text NOT NULL CHECK(classification IN ('audio_host','video_host')),
 effective_from timestamptz NOT NULL,
 effective_until timestamptz,
 CHECK(account_id<>agency_id),CHECK(effective_until IS NULL OR effective_until>effective_from)
);
CREATE UNIQUE INDEX agency_host_current ON app.agency_host_associations(account_id) WHERE effective_until IS NULL;
CREATE INDEX agency_host_effective ON app.agency_host_associations(account_id,effective_from DESC,id);
-- +goose StatementBegin
CREATE FUNCTION app.agency_history_guard() RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
 PERFORM pg_advisory_xact_lock(772341909);
 IF TG_OP='UPDATE' AND OLD.effective_until IS NOT NULL THEN RAISE EXCEPTION 'closed association is immutable'; END IF;
 IF NEW.effective_until IS NOT NULL AND NEW.effective_until<=NEW.effective_from THEN RAISE EXCEPTION 'invalid association dates'; END IF;
 IF EXISTS(SELECT 1 FROM app.agency_host_associations a WHERE a.account_id=NEW.account_id AND a.id<>NEW.id AND tstzrange(a.effective_from,a.effective_until,'[)') && tstzrange(NEW.effective_from,NEW.effective_until,'[)')) THEN RAISE EXCEPTION 'overlapping association history'; END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER agency_history_integrity BEFORE INSERT OR UPDATE ON app.agency_host_associations FOR EACH ROW EXECUTE FUNCTION app.agency_history_guard();
CREATE TABLE app.wallet_withdrawals (
 id text PRIMARY KEY CHECK(id ~ '^[0-9a-f]{32}$'),
 account_id text NOT NULL REFERENCES app.identity_accounts(id),
 reserve_operation_id text NOT NULL UNIQUE REFERENCES app.wallet_operations(id),
 amount bigint NOT NULL CHECK(amount>0),fee bigint NOT NULL CHECK(fee>=0),reserved bigint NOT NULL CHECK(reserved>0 AND reserved::numeric=amount::numeric+fee::numeric),
 policy_id text NOT NULL REFERENCES app.economy_policies(id),
 eligibility_snapshot jsonb NOT NULL CHECK(octet_length(eligibility_snapshot::text)<=16384),
 state text NOT NULL CHECK(state IN ('pending','approved','rejected','cancelled')),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX wallet_withdrawals_owner_page ON app.wallet_withdrawals(account_id,id);
CREATE TABLE app.wallet_withdrawal_events (
 id text PRIMARY KEY CHECK(id ~ '^[0-9a-f]{32}$'),
 withdrawal_id text NOT NULL REFERENCES app.wallet_withdrawals(id),actor_id text NOT NULL REFERENCES app.identity_accounts(id),
 action text NOT NULL CHECK(action IN ('approve','reject','cancel')),
 resulting_state text NOT NULL CHECK(resulting_state IN ('approved','rejected','cancelled')),
 idempotency_key text NOT NULL CHECK(octet_length(idempotency_key) BETWEEN 1 AND 64),
 fingerprint text NOT NULL CHECK(fingerprint ~ '^[0-9a-f]{64}$'),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(actor_id,idempotency_key)
);
-- Runtime has no UPDATE balances, INSERT entries, or UPDATE/DELETE financial history.
-- +goose StatementBegin
CREATE FUNCTION app.wallet_post(op text, ids text[], amounts bigint[]) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE i integer; n integer; old_balance bigint; account_kind text; next_balance numeric; operation app.wallet_operations%ROWTYPE; expected bigint; agency_owner text;
BEGIN
 n:=cardinality(ids);
 IF ids IS NULL OR amounts IS NULL OR n<2 OR n>8 OR n<>cardinality(amounts) OR array_position(ids,NULL) IS NOT NULL OR array_position(amounts,NULL) IS NOT NULL OR (SELECT count(DISTINCT x) FROM unnest(ids) x)<>n OR EXISTS(SELECT 1 FROM unnest(amounts) x WHERE x=0) OR (SELECT sum(x::numeric) FROM unnest(amounts) x)<>0 THEN RAISE EXCEPTION 'invalid wallet postings'; END IF;
 IF NOT EXISTS(SELECT 1 FROM app.wallet_operations WHERE id=op) OR EXISTS(SELECT 1 FROM app.wallet_entries WHERE operation_id=op) THEN RAISE EXCEPTION 'invalid wallet operation'; END IF;
 SELECT * INTO STRICT operation FROM app.wallet_operations WHERE id=op;
 IF operation.policy_id IS NOT NULL AND operation.kind<>'withdrawal_release' AND NOT EXISTS(SELECT 1 FROM app.economy_policies p WHERE p.id=operation.policy_id AND p.config=operation.policy_snapshot AND p.effective_at<=operation.created_at) THEN RAISE EXCEPTION 'invalid policy snapshot'; END IF;
 IF operation.kind IN ('transfer','gift','withdrawal_reserve') AND (operation.policy_id IS NULL OR NOT EXISTS(SELECT 1 FROM app.economy_policies p WHERE p.id=operation.policy_id AND p.kind=CASE WHEN operation.kind='withdrawal_reserve' THEN 'withdrawal' ELSE operation.kind END AND p.config=operation.policy_snapshot AND p.effective_at<=operation.created_at)) THEN RAISE EXCEPTION 'invalid operation policy'; END IF;
 IF operation.kind='adjustment' AND NOT EXISTS(SELECT 1 FROM app.access_owners WHERE account_id=operation.actor_id) THEN RAISE EXCEPTION 'issuance requires protected authority'; END IF;
 IF operation.kind IN ('transfer','gift') AND operation.actor_id=operation.target_id THEN RAISE EXCEPTION 'self spending prohibited'; END IF;
 IF operation.kind='gift' THEN SELECT agency_id INTO agency_owner FROM app.agency_host_associations WHERE account_id=operation.target_id AND effective_from<=operation.created_at AND (effective_until IS NULL OR effective_until>operation.created_at) ORDER BY effective_from DESC LIMIT 1; END IF;
 FOR i IN 1..n LOOP
  expected:=NULL;
  IF operation.kind='transfer' THEN
   IF ids[i]='user:'||operation.actor_id THEN expected:=-(operation.amount+operation.fee); ELSIF ids[i]='user:'||operation.target_id THEN expected:=operation.amount; ELSIF ids[i]='platform:fees' THEN expected:=operation.fee; END IF;
  ELSIF operation.kind='withdrawal_reserve' THEN
   IF operation.actor_id<>operation.target_id THEN RAISE EXCEPTION 'invalid reservation owner'; END IF;
   IF ids[i]='user:'||operation.actor_id THEN expected:=-(operation.amount+operation.fee); ELSIF ids[i]='held:'||operation.actor_id THEN expected:=operation.amount+operation.fee; END IF;
  ELSIF operation.kind='withdrawal_release' THEN
   IF NOT EXISTS(SELECT 1 FROM app.wallet_withdrawals w WHERE w.id=operation.reference AND w.account_id=operation.target_id AND w.reserved=operation.amount AND w.policy_id=operation.policy_id AND operation.fee=0 AND w.state IN ('pending','approved')) THEN RAISE EXCEPTION 'invalid reservation release'; END IF;
   IF ids[i]='user:'||operation.target_id THEN expected:=operation.amount; ELSIF ids[i]='held:'||operation.target_id THEN expected:=-operation.amount; END IF;
  ELSIF operation.kind='adjustment' THEN
   IF ids[i] IN ('user:'||operation.target_id,'platform:issuance') AND abs(amounts[i]::numeric)=operation.amount THEN expected:=amounts[i]; END IF;
  ELSIF operation.kind='gift' THEN
   IF ids[i]='user:'||operation.actor_id THEN expected:=-operation.amount;
   ELSIF amounts[i]>0 AND ids[i] IN ('user:'||operation.target_id,'user:'||agency_owner,'platform:gifts','platform:tax') THEN expected:=amounts[i]; END IF;
  END IF;
  IF expected IS NULL OR expected<>amounts[i] THEN RAISE EXCEPTION 'posting does not match operation'; END IF;
 END LOOP;
 PERFORM id FROM app.wallet_accounts WHERE id=ANY(ids) ORDER BY id FOR UPDATE;
 FOR i IN 1..n LOOP
  SELECT balance,kind INTO STRICT old_balance,account_kind FROM app.wallet_accounts WHERE id=ids[i];
  next_balance:=old_balance::numeric+amounts[i]::numeric;
  IF next_balance>9223372036854775807 OR next_balance<0 AND account_kind<>'issuance' OR next_balance< -9223372036854775807 OR account_kind='issuance' AND next_balance>0 THEN RAISE EXCEPTION USING ERRCODE='P0002', MESSAGE='wallet range or funds violation'; END IF;
  UPDATE app.wallet_accounts SET balance=next_balance::bigint WHERE id=ids[i];
  INSERT INTO app.wallet_entries(operation_id,account_id,delta) VALUES(op,ids[i],amounts[i]);
 END LOOP;
END $$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION app.wallet_post(text,text[],bigint[]) FROM PUBLIC;
-- +goose StatementBegin
CREATE FUNCTION app.wallet_operation_balanced() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 IF NEW.kind='adjustment' AND NOT EXISTS(SELECT 1 FROM app.access_audit WHERE actor=NEW.actor_id AND target=NEW.target_id AND action='financial.adjust' AND fields @> ARRAY['operation:'||NEW.id]) THEN RAISE EXCEPTION 'adjustment audit required'; END IF;
 IF (SELECT count(*) FROM app.wallet_entries WHERE operation_id=NEW.id)<2 OR (SELECT COALESCE(sum(delta::numeric),1) FROM app.wallet_entries WHERE operation_id=NEW.id)<>0 THEN RAISE EXCEPTION 'unbalanced wallet operation'; END IF;
 RETURN NULL;
END $$;
-- +goose StatementEnd
CREATE CONSTRAINT TRIGGER wallet_operation_conservation AFTER INSERT ON app.wallet_operations DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION app.wallet_operation_balanced();
-- +goose StatementBegin
DO $$
DECLARE runtime_role text := current_database() || '_app';
BEGIN
 EXECUTE format('GRANT SELECT ON app.wallet_accounts,app.wallet_entries,app.wallet_operations,app.economy_policies,app.agency_host_associations,app.wallet_withdrawals,app.wallet_withdrawal_events,app.economy_commands TO %I',runtime_role);
 EXECUTE format('GRANT INSERT(id,owner_id,kind) ON app.wallet_accounts TO %I',runtime_role);
 EXECUTE format('GRANT USAGE ON SEQUENCE app.economy_policies_revision_seq TO %I',runtime_role);
 EXECUTE format('GRANT INSERT ON app.wallet_operations,app.economy_policies,app.agency_host_associations,app.wallet_withdrawals,app.wallet_withdrawal_events,app.economy_commands TO %I',runtime_role);
 EXECUTE format('GRANT UPDATE(effective_until) ON app.agency_host_associations TO %I',runtime_role);
 EXECUTE format('GRANT UPDATE(state) ON app.wallet_withdrawals TO %I',runtime_role);
 EXECUTE format('GRANT EXECUTE ON FUNCTION app.wallet_post(text,text[],bigint[]) TO %I',runtime_role);
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN IF EXISTS(SELECT 1 FROM app.wallet_operations) OR EXISTS(SELECT 1 FROM app.access_role_permissions WHERE permission NOT IN ('users.read','users.private.read','users.profile.update','users.restrict')) THEN RAISE EXCEPTION 'financial rollback requires separately reviewed data recovery'; END IF; END $$;
-- +goose StatementEnd
DROP TABLE app.economy_commands;
DROP TABLE app.wallet_withdrawal_events;
DROP TABLE app.wallet_withdrawals;
DROP TRIGGER agency_history_integrity ON app.agency_host_associations;
DROP FUNCTION app.agency_history_guard();
DROP TABLE app.agency_host_associations;
DROP TRIGGER wallet_operation_conservation ON app.wallet_operations;
DROP FUNCTION app.wallet_operation_balanced();
DROP FUNCTION app.wallet_post(text,text[],bigint[]);
DROP TABLE app.wallet_entries;
DROP TABLE app.wallet_operations;
DROP TABLE app.economy_policies;
DROP TABLE app.wallet_accounts;
ALTER TABLE app.access_role_permissions DROP CONSTRAINT access_role_permissions_permission_check;
ALTER TABLE app.access_role_permissions ADD CONSTRAINT access_role_permissions_permission_check CHECK(permission IN ('users.read','users.private.read','users.profile.update','users.restrict'));
