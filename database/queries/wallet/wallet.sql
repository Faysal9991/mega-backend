-- name: Initialize :exec
INSERT INTO app.wallet_accounts(id,owner_id,kind) VALUES('user:'||sqlc.arg(account_id)::text,sqlc.arg(account_id),'user'),('held:'||sqlc.arg(account_id)::text,sqlc.arg(account_id),'held') ON CONFLICT(id) DO NOTHING;
-- name: Wallet :one
SELECT u.balance AS available,h.balance AS held FROM app.wallet_accounts u JOIN app.wallet_accounts h ON h.owner_id=u.owner_id AND h.kind='held' WHERE u.owner_id=$1 AND u.kind='user';
-- name: AccountBalance :one
SELECT balance FROM app.wallet_accounts WHERE id=$1;
-- name: Post :exec
SELECT app.wallet_post($1::text,$2::text[],$3::bigint[]);
-- name: Operation :one
SELECT id,kind,actor_id,target_id,amount,fee,COALESCE(policy_id,'')::text AS policy_id,reference,created_at,policy_snapshot FROM app.wallet_operations WHERE id=$1;
-- name: InsertOperation :exec
INSERT INTO app.wallet_operations(id,kind,actor_id,target_id,idempotency_key,fingerprint,amount,fee,policy_id,policy_snapshot,reference,created_at) VALUES(sqlc.arg(id),sqlc.arg(kind),sqlc.arg(actor_id),sqlc.arg(target_id),sqlc.arg(idempotency_key),sqlc.arg(fingerprint),sqlc.arg(amount),sqlc.arg(fee),NULLIF(sqlc.arg(policy_id)::text,''),sqlc.arg(policy_snapshot)::text::jsonb,sqlc.arg(reference),sqlc.arg(created_at));
-- name: Entries :many
SELECT account_id,delta FROM app.wallet_entries WHERE operation_id=$1 ORDER BY account_id;
-- name: History :many
SELECT DISTINCT o.id,o.kind,o.actor_id,o.target_id,o.amount,o.fee,COALESCE(o.policy_id,'')::text AS policy_id,o.reference,o.created_at FROM app.wallet_operations o JOIN app.wallet_entries e ON e.operation_id=o.id JOIN app.wallet_accounts a ON a.id=e.account_id WHERE a.owner_id=sqlc.arg(account_id) AND o.id>sqlc.arg(cursor) AND (sqlc.arg(kind)::text='' OR o.kind=sqlc.arg(kind)) ORDER BY o.id LIMIT sqlc.arg(page_limit);
-- name: PeriodTotal :one
SELECT COALESCE(sum(amount),0)::text AS total FROM app.wallet_operations WHERE actor_id=$1 AND kind=$2 AND created_at >= $3 AND created_at<$4;
-- name: ActivePolicy :one
SELECT id,config FROM app.economy_policies WHERE kind=$1 AND scope=$2 AND effective_at<=clock_timestamp() ORDER BY effective_at DESC,revision DESC LIMIT 1;
-- name: LatestPolicy :one
SELECT id,effective_at FROM app.economy_policies WHERE kind=$1 AND scope=$2 ORDER BY revision DESC LIMIT 1;
-- name: InsertPolicy :exec
INSERT INTO app.economy_policies(id,kind,scope,previous_id,effective_at,actor_id,config) VALUES(sqlc.arg(id),sqlc.arg(kind),sqlc.arg(scope),NULLIF(sqlc.arg(previous_id)::text,''),sqlc.arg(effective_at),sqlc.arg(actor_id),sqlc.arg(config)::text::jsonb);
-- name: Policies :many
SELECT config FROM app.economy_policies WHERE id>$1 ORDER BY id LIMIT $2;
-- name: CurrentAssociation :one
SELECT id,account_id,agency_id,classification,effective_from,effective_until FROM app.agency_host_associations WHERE account_id=$1 AND effective_from<=clock_timestamp() AND (effective_until IS NULL OR effective_until>clock_timestamp()) ORDER BY effective_from DESC,id DESC LIMIT 1;
-- name: AssociationHistory :many
SELECT id,account_id,agency_id,classification,effective_from,effective_until FROM app.agency_host_associations WHERE account_id=$1 AND id>$2 ORDER BY id LIMIT $3;
-- name: InsertWithdrawal :exec
INSERT INTO app.wallet_withdrawals(id,account_id,reserve_operation_id,amount,fee,reserved,policy_id,eligibility_snapshot,state) VALUES(sqlc.arg(id),sqlc.arg(account_id),sqlc.arg(reserve_operation_id),sqlc.arg(amount),sqlc.arg(fee),sqlc.arg(reserved),sqlc.arg(policy_id),sqlc.arg(eligibility_snapshot)::text::jsonb,'pending');
-- name: Withdrawal :one
SELECT id,account_id,amount,fee,reserved,policy_id,state,created_at FROM app.wallet_withdrawals WHERE id=$1 FOR UPDATE;
-- name: Withdrawals :many
SELECT id,account_id,amount,fee,reserved,policy_id,state,created_at FROM app.wallet_withdrawals WHERE (sqlc.arg(account_id)::text='' OR account_id=sqlc.arg(account_id)) AND id>sqlc.arg(cursor)::text ORDER BY id LIMIT sqlc.arg(page_limit);
-- name: Transition :exec
UPDATE app.wallet_withdrawals SET state=$2 WHERE id=$1;
-- name: InsertWithdrawalEvent :exec
INSERT INTO app.wallet_withdrawal_events(id,withdrawal_id,actor_id,action,resulting_state,idempotency_key,fingerprint) VALUES($1,$2,$3,$4,$5,$6,$7);
-- name: Reconcile :many
SELECT a.id,a.balance,COALESCE(sum(e.delta),0)::text AS ledger_balance FROM app.wallet_accounts a LEFT JOIN app.wallet_entries e ON e.account_id=a.id GROUP BY a.id HAVING a.balance::numeric<>COALESCE(sum(e.delta::numeric),0) ORDER BY a.id LIMIT 100;
-- name: SchemaReady :one
SELECT count(*)=8 AND EXISTS(SELECT 1 FROM pg_catalog.pg_proc WHERE oid=to_regprocedure('app.wallet_post(text,text[],bigint[])') AND prosecdef) AND EXISTS(SELECT 1 FROM pg_catalog.pg_proc WHERE oid=to_regprocedure('app.wallet_operation_balanced()') AND prosecdef) AND EXISTS(SELECT 1 FROM pg_catalog.pg_trigger WHERE tgrelid=to_regclass('app.wallet_operations') AND tgname='wallet_operation_conservation' AND tgenabled<>'D') AND EXISTS(SELECT 1 FROM pg_catalog.pg_trigger WHERE tgrelid=to_regclass('app.agency_host_associations') AND tgname='agency_history_integrity' AND tgenabled<>'D') FROM unnest(ARRAY['app.economy_commands','app.wallet_accounts','app.wallet_entries','app.wallet_operations','app.economy_policies','app.agency_host_associations','app.wallet_withdrawals','app.wallet_withdrawal_events']) AS names(name) WHERE to_regclass(name) IS NOT NULL;

-- name: Participant :one
SELECT EXISTS(SELECT 1 FROM app.wallet_entries e JOIN app.wallet_accounts a ON a.id=e.account_id WHERE e.operation_id=$1 AND a.owner_id=$2);

-- name: ExistingCommand :one
SELECT fingerprint,result FROM app.economy_commands WHERE actor_id=$1 AND kind=$2 AND idempotency_key=$3;
-- name: SaveCommand :exec
INSERT INTO app.economy_commands(actor_id,kind,idempotency_key,fingerprint,result) VALUES(sqlc.arg(actor_id),sqlc.arg(kind),sqlc.arg(idempotency_key),sqlc.arg(fingerprint),sqlc.arg(result)::text::jsonb);
-- name: Clock :one
SELECT clock_timestamp()::timestamptz AS now;
-- name: PackageAccountingConfigured :one
SELECT EXISTS(SELECT 1 FROM app.package_financial_policies WHERE id='00000000000000000000000000000002' AND mode='platform_full_no_vat');
