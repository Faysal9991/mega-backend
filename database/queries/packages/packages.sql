-- name: GetPackage :one
SELECT p.id,p.code,v.id AS version_id,v.name,v.description,v.price,v.duration_seconds,v.powers,v.badge_asset_id,v.entrance_asset_id,v.listing_priority,v.active,v.created_at FROM app.package_catalog p JOIN app.package_versions v ON v.id=p.current_version WHERE p.id=$1;
-- name: Packages :many
SELECT p.id,p.code,v.id AS version_id,v.name,v.description,v.price,v.duration_seconds,v.powers,v.badge_asset_id,v.entrance_asset_id,v.listing_priority,v.active,v.created_at FROM app.package_catalog p JOIN app.package_versions v ON v.id=p.current_version WHERE p.id>$1 AND (v.active OR sqlc.arg(admin)::boolean) ORDER BY p.id LIMIT sqlc.arg(page_limit);
-- name: CreatePackage :exec
INSERT INTO app.package_catalog(id,code) VALUES($1,$2);
-- name: InsertVersion :exec
INSERT INTO app.package_versions(id,package_id,name,description,price,duration_seconds,powers,badge_asset_id,entrance_asset_id,listing_priority,active) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11);
-- name: SwitchVersion :one
UPDATE app.package_catalog SET current_version=$2 WHERE id=$1 AND current_version IS NOT DISTINCT FROM sqlc.narg(previous)::text RETURNING id;
-- name: Entitlements :many
SELECT e.id,e.account_id,e.package_id,e.starts_at,e.expires_at,e.source,v.name,v.powers,v.listing_priority FROM app.package_entitlements e JOIN app.package_catalog p ON p.id=e.package_id JOIN app.package_versions v ON v.id=p.current_version WHERE e.account_id=$1 AND e.id>$2 AND e.starts_at<=clock_timestamp() AND e.expires_at>clock_timestamp() AND v.active AND NOT EXISTS(SELECT 1 FROM app.package_revocations r WHERE r.entitlement_id=e.id) ORDER BY e.id LIMIT sqlc.arg(page_limit);
-- name: InsertEntitlement :exec
INSERT INTO app.package_entitlements(id,account_id,package_id,starts_at,expires_at,source) VALUES($1,$2,$3,$4,$5,$6);
-- name: RevokeEntitlement :one
INSERT INTO app.package_revocations(entitlement_id,actor_id) SELECT id,$2 FROM app.package_entitlements WHERE id=$1 ON CONFLICT(entitlement_id) DO NOTHING RETURNING entitlement_id;
-- name: ExistingCommand :one
SELECT fingerprint,result FROM app.package_commands WHERE actor_id=$1 AND kind=$2 AND key=$3;
-- name: SaveCommand :exec
INSERT INTO app.package_commands(actor_id,kind,key,fingerprint,result) VALUES($1,$2,$3,$4,sqlc.arg(result)::text::jsonb);
-- name: VersionHistory :many
SELECT id,package_id,name,description,price,duration_seconds,powers,badge_asset_id,entrance_asset_id,listing_priority,active,created_at FROM app.package_versions WHERE package_id=$1 AND id>$2 ORDER BY id LIMIT sqlc.arg(page_limit);
-- name: Subscribers :many
SELECT e.id,e.account_id,e.package_id,e.starts_at,e.expires_at,e.source FROM app.package_entitlements e JOIN app.package_catalog p ON p.id=e.package_id JOIN app.package_versions v ON v.id=p.current_version WHERE e.package_id=$1 AND e.id>$2 AND e.starts_at<=clock_timestamp() AND e.expires_at>clock_timestamp() AND v.active AND NOT EXISTS(SELECT 1 FROM app.package_revocations r WHERE r.entitlement_id=e.id) ORDER BY e.id LIMIT sqlc.arg(page_limit);
-- name: Purchases :many
SELECT p.operation_id,p.account_id,p.package_id,p.version_id,p.entitlement_id,p.price,p.created_at,e.starts_at,e.expires_at FROM app.package_purchases p JOIN app.package_entitlements e ON e.id=p.entitlement_id WHERE (p.account_id=sqlc.arg(account_id) OR sqlc.arg(admin)::boolean) AND p.operation_id>sqlc.arg(cursor) ORDER BY p.operation_id LIMIT sqlc.arg(page_limit);
-- name: Clock :one
SELECT clock_timestamp()::timestamptz AS now;
-- name: ActiveEntitlementCount :one
SELECT count(*) FROM (SELECT e.id FROM app.package_entitlements e WHERE e.account_id=$1 AND e.expires_at>clock_timestamp() AND NOT EXISTS(SELECT 1 FROM app.package_revocations r WHERE r.entitlement_id=e.id) LIMIT 101) bounded;
-- name: UsablePackageAsset :one
SELECT EXISTS(SELECT 1 FROM app.delivery_assets WHERE id=$1 AND state='ready' AND purpose='gift_thumbnail');
-- name: InsertPurchase :exec
INSERT INTO app.package_purchases(operation_id,account_id,package_id,version_id,entitlement_id,price,created_at) VALUES($1,$2,$3,$4,$5,$6,$7);
-- name: AccountingConfigured :one
SELECT EXISTS(SELECT 1 FROM app.package_financial_policies WHERE id='00000000000000000000000000000002' AND mode='platform_full_no_vat');
-- name: Ready :one
SELECT to_regclass('app.package_profit') IS NOT NULL AND to_regclass('app.access_policy_head') IS NOT NULL AND to_regclass('app.live_list_snapshots') IS NOT NULL AND to_regprocedure('app.wallet_post_existing(text,text[],bigint[])') IS NOT NULL AND (SELECT count(*) FROM app.access_capabilities)=sqlc.arg(capability_count)::bigint AND EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema='app' AND table_name='live_operations' AND column_name='lease_token') AND EXISTS(SELECT 1 FROM pg_catalog.pg_trigger WHERE tgrelid=to_regclass('app.package_catalog') AND tgname='capability_packages' AND tgenabled<>'D');

-- name: HasUnexpiredPackage :one
SELECT EXISTS(SELECT 1 FROM app.package_entitlements e WHERE account_id=$1 AND package_id=$2 AND expires_at>clock_timestamp() AND NOT EXISTS(SELECT 1 FROM app.package_revocations r WHERE r.entitlement_id=e.id));

-- name: RenewalStart :one
SELECT COALESCE(max(e.expires_at),sqlc.arg(now)::timestamptz)::timestamptz AS starts_at FROM app.package_entitlements e WHERE e.account_id=$1 AND e.package_id=$2 AND e.expires_at>sqlc.arg(now)::timestamptz AND NOT EXISTS(SELECT 1 FROM app.package_revocations r WHERE r.entitlement_id=e.id);
