-- name: Ready :one
SELECT to_regclass('app.delivery_outbox') IS NOT NULL AND to_regclass('app.room_gifts') IS NOT NULL AND to_regclass('app.push_devices') IS NOT NULL AND to_regclass('app.package_versions') IS NOT NULL AND to_regclass('app.access_policy_head') IS NOT NULL AND EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema='app' AND table_name='live_members' AND column_name='private_entry') AND EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema='app' AND table_name='live_operations' AND column_name='lease_token');
-- name: Clock :one
SELECT clock_timestamp()::timestamptz AS now;
-- name: Command :one
SELECT fingerprint,result FROM app.delivery_commands WHERE actor_id=$1 AND kind=$2 AND key=$3;
-- name: SaveCommand :exec
INSERT INTO app.delivery_commands(actor_id,kind,key,fingerprint,result) VALUES($1,$2,$3,$4,sqlc.arg(result)::text::jsonb);
-- name: Enqueue :exec
INSERT INTO app.delivery_outbox(id,topic,aggregate_id,payload,available_at) VALUES($1,$2,$3,sqlc.arg(payload)::text::jsonb,$4) ON CONFLICT(topic,aggregate_id) DO NOTHING;
-- name: Claim :one
UPDATE app.delivery_outbox SET state='running',attempts=attempts+1,lease_id=$1,lease_until=clock_timestamp()+interval '60 seconds',updated_at=clock_timestamp()
WHERE id=(SELECT id FROM app.delivery_outbox WHERE topic=ANY($2::text[]) AND available_at<=clock_timestamp() AND attempts<10 AND (state='pending' OR state='running' AND lease_until<clock_timestamp()) ORDER BY available_at,id FOR UPDATE SKIP LOCKED LIMIT 1)
RETURNING id,topic,aggregate_id,payload,attempts,lease_id;
-- name: Ack :execrows
UPDATE app.delivery_outbox SET state=$3,error_code=$4,available_at=clock_timestamp()+make_interval(secs=>sqlc.arg(delay_seconds)::integer),lease_until=NULL,updated_at=clock_timestamp() WHERE id=$1 AND lease_id=$2 AND state='running';
-- name: ExpireExhausted :exec
UPDATE app.delivery_outbox SET state='failed',error_code='RETRY_EXHAUSTED',updated_at=clock_timestamp() WHERE id IN (SELECT id FROM app.delivery_outbox WHERE state='running' AND lease_until<clock_timestamp() AND attempts=10 LIMIT 100);
-- name: Delivery :one
SELECT id,topic,aggregate_id,state,attempts,error_code,created_at,updated_at FROM app.delivery_outbox WHERE id=$1;
-- name: Deliveries :many
SELECT id,topic,aggregate_id,state,attempts,error_code,created_at,updated_at FROM app.delivery_outbox WHERE id>$1 ORDER BY id LIMIT $2;
-- name: Gift :one
SELECT c.id,c.code,v.id AS version_id,v.name,v.description,v.price,v.category,v.thumbnail_asset_id,v.thumbnail_url,v.animation_url,v.active,v.display_order,c.created_at,v.created_at AS updated_at FROM app.gift_catalog c JOIN app.gift_versions v ON v.id=c.current_version WHERE c.id=$1;
-- name: Gifts :many
SELECT c.id,c.code,v.id AS version_id,v.name,v.description,v.price,v.category,v.thumbnail_asset_id,v.thumbnail_url,v.animation_url,v.active,v.display_order,c.created_at,v.created_at AS updated_at FROM app.gift_catalog c JOIN app.gift_versions v ON v.id=c.current_version WHERE (sqlc.arg(cursor)::text='' OR (c.display_order,c.id)>(COALESCE((SELECT display_order FROM app.gift_catalog WHERE id=sqlc.arg(cursor)::text),-1),sqlc.arg(cursor)::text)) AND (sqlc.arg(admin)::boolean OR c.active) ORDER BY c.display_order,c.id LIMIT sqlc.arg(page_limit)::integer;
-- name: CreateGift :exec
INSERT INTO app.gift_catalog(id,code) VALUES($1,$2);
-- name: VersionGift :exec
INSERT INTO app.gift_versions(id,gift_id,name,description,price,category,thumbnail_asset_id,thumbnail_url,animation_url,active,display_order) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11);
-- name: PointGift :exec
UPDATE app.gift_catalog c SET current_version=v.id,display_order=v.display_order,active=v.active FROM app.gift_versions v WHERE c.id=sqlc.arg(gift_id)::text AND v.id=sqlc.arg(version_id)::text AND v.gift_id=c.id;
-- name: RecipientPolicy :one
SELECT id,previous_id,mode,created_at FROM app.gift_recipient_policies ORDER BY revision DESC LIMIT 1;
-- name: PublishRecipientPolicy :exec
INSERT INTO app.gift_recipient_policies(id,previous_id,mode) VALUES($1,$2,'active_publishers');
-- name: RecordGift :exec
INSERT INTO app.room_gifts(operation_id,gift_id,version_id,room_id,session_id,sender_id,recipient_id,event) VALUES($1,$2,$3,$4,$5,$6,$7,sqlc.arg(event)::text::jsonb);
-- name: GiftHistory :many
SELECT g.operation_id,g.event,o.state AS delivery_state FROM app.room_gifts g JOIN app.delivery_outbox o ON o.id=g.operation_id WHERE g.operation_id>$1 AND (g.sender_id=$2 OR g.recipient_id=$2 OR sqlc.arg(admin)::boolean) ORDER BY g.operation_id LIMIT $3;
-- name: LiveSession :one
SELECT r.state='open' AND r.session_id=$2 AS active,EXISTS(SELECT 1 FROM app.live_operations o WHERE o.room_id=r.id AND o.state<>'done') AS synchronizing FROM app.live_rooms r WHERE r.id=$1;
-- name: PublicNames :many
SELECT account_id,display_name FROM app.profiles WHERE account_id=ANY($1::text[]) LIMIT 2;
-- name: Asset :one
SELECT * FROM app.delivery_assets WHERE id=$1;
-- name: AssetQuota :one
SELECT count(*)::integer AS count,COALESCE(sum(declared_size),0)::bigint AS total FROM (SELECT declared_size FROM app.delivery_assets WHERE owner_id=$1 AND state<>'deleted' LIMIT 101) a;
-- name: InsertAsset :exec
INSERT INTO app.delivery_assets(id,owner_id,purpose,target_id,object_key,declared_type,declared_size,checksum) VALUES($1,$2,$3,$4,$5,$6,$7,$8);
-- name: ClaimAsset :one
UPDATE app.delivery_assets SET state='processing',lease_id=$2,lease_until=clock_timestamp()+interval '2 minutes',updated_at=clock_timestamp() WHERE id=$1 AND expires_at>clock_timestamp() AND (state='pending' OR state='processing' AND lease_until<clock_timestamp()) RETURNING *;
-- name: FinishAsset :execrows
UPDATE app.delivery_assets SET state='ready',approved_key=$3,thumbnail_key=$4,public_url=$5,width=$6,height=$7,size_bytes=$8,error_code='',lease_until=NULL,updated_at=clock_timestamp() WHERE id=$1 AND lease_id=$2 AND state='processing';
-- name: FailAsset :exec
UPDATE app.delivery_assets SET state=$3,error_code=$4,lease_until=NULL,updated_at=clock_timestamp() WHERE id=$1 AND lease_id=$2 AND state='processing';
-- name: AssetLink :one
SELECT asset_id FROM app.delivery_asset_links WHERE target_id=$1 AND purpose=$2;
-- name: LinkAsset :exec
INSERT INTO app.delivery_asset_links(target_id,purpose,asset_id) VALUES($1,$2,$3) ON CONFLICT(target_id,purpose) DO UPDATE SET asset_id=EXCLUDED.asset_id;
-- name: AssetReferenced :one
SELECT EXISTS(SELECT 1 FROM app.delivery_asset_links WHERE asset_id=$1) OR EXISTS(SELECT 1 FROM app.gift_versions WHERE thumbnail_asset_id=$1) OR EXISTS(SELECT 1 FROM app.package_versions WHERE badge_asset_id=$1 OR entrance_asset_id=$1) AS referenced;
-- name: RetireAsset :exec
UPDATE app.delivery_assets SET state='retiring',updated_at=clock_timestamp() WHERE id=$1 AND state<>'deleted';
-- name: DeleteAsset :exec
UPDATE app.delivery_assets SET state='deleted',updated_at=clock_timestamp() WHERE id=$1 AND state='retiring';
-- name: AgencyExists :one
SELECT id FROM app.agencies WHERE id=$1;
-- name: CurrentSession :one
SELECT id FROM app.identity_sessions WHERE account_id=$1 AND installation_id=$2 AND access_hash=$3 AND NOT revoked AND expires_at>clock_timestamp() AND access_expires_at>clock_timestamp();
-- name: Devices :one
SELECT count(*)::integer FROM (SELECT installation_id FROM app.push_devices WHERE account_id=$1 LIMIT 11) d;
-- name: Device :one
SELECT installation_id,account_id,session_id FROM app.push_devices WHERE installation_id=$1;
-- name: RegisterDevice :exec
INSERT INTO app.push_devices(installation_id,account_id,session_id,platform,token_hash,token_cipher,expires_at) VALUES($1,$2,$3,$4,$5,$6,LEAST(clock_timestamp()+interval '60 days',(SELECT expires_at FROM app.identity_sessions WHERE id=$3))) ON CONFLICT(installation_id) DO UPDATE SET account_id=EXCLUDED.account_id,session_id=EXCLUDED.session_id,platform=EXCLUDED.platform,token_hash=EXCLUDED.token_hash,token_cipher=EXCLUDED.token_cipher,revision=DEFAULT,expires_at=EXCLUDED.expires_at,updated_at=clock_timestamp() WHERE push_devices.account_id<>EXCLUDED.account_id OR push_devices.session_id<>EXCLUDED.session_id OR push_devices.token_hash<>EXCLUDED.token_hash OR push_devices.platform<>EXCLUDED.platform;
-- name: RemoveDevice :exec
DELETE FROM app.push_devices WHERE installation_id=$1 AND account_id=$2;
-- name: UnbindInstallation :exec
DELETE FROM app.push_devices WHERE installation_id=$1;
-- name: UnbindSession :exec
DELETE FROM app.push_devices WHERE session_id=$1;
-- name: UnbindAccount :exec
DELETE FROM app.push_devices WHERE account_id=$1;
-- name: Preferences :one
SELECT * FROM app.notification_preferences WHERE account_id=$1;
-- name: PutPreferences :exec
INSERT INTO app.notification_preferences(account_id,gift_received,agency_decision,account_event,room_invitation,admin_announcement) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(account_id) DO UPDATE SET gift_received=EXCLUDED.gift_received,agency_decision=EXCLUDED.agency_decision,account_event=EXCLUDED.account_event,room_invitation=EXCLUDED.room_invitation,admin_announcement=EXCLUDED.admin_announcement;
-- name: Notification :exec
INSERT INTO app.notifications(id,account_id,kind,reference) VALUES($1,$2,$3,$4) ON CONFLICT(account_id,kind,reference) DO NOTHING;
-- name: NotifyMany :many
INSERT INTO app.notifications(id,account_id,kind,reference) SELECT md5(sqlc.arg(reference)::text||a.id),a.id,'admin_announcement',sqlc.arg(reference)::text FROM app.identity_accounts a WHERE a.id=ANY(sqlc.arg(targets)::text[]) AND a.status='active' RETURNING id,account_id;
-- name: QueuePush :exec
INSERT INTO app.delivery_outbox(id,topic,aggregate_id,payload)
SELECT md5(n.id||d.installation_id||d.revision::text),'push',n.id||':'||d.installation_id||':'||d.revision::text,jsonb_build_object('notification_id',n.id,'installation_id',d.installation_id,'revision',d.revision)
FROM app.notifications n JOIN app.push_devices d ON d.account_id=n.account_id JOIN app.notification_preferences p ON p.account_id=n.account_id
WHERE n.id=$1 AND CASE n.kind WHEN 'gift_received' THEN p.gift_received WHEN 'agency_decision' THEN p.agency_decision WHEN 'account_event' THEN p.account_event WHEN 'room_invitation' THEN p.room_invitation WHEN 'admin_announcement' THEN p.admin_announcement ELSE false END ORDER BY d.installation_id LIMIT 10 ON CONFLICT(topic,aggregate_id) DO NOTHING;
-- name: QueueManyPush :exec
INSERT INTO app.delivery_outbox(id,topic,aggregate_id,payload)
SELECT md5(n.id||d.installation_id||d.revision::text),'push',n.id||':'||d.installation_id||':'||d.revision::text,jsonb_build_object('notification_id',n.id,'installation_id',d.installation_id,'revision',d.revision)
FROM app.notifications n JOIN app.push_devices d ON d.account_id=n.account_id JOIN app.notification_preferences p ON p.account_id=n.account_id
WHERE n.id=ANY($1::text[]) AND n.kind='admin_announcement' AND p.admin_announcement ORDER BY n.id,d.installation_id LIMIT 1000 ON CONFLICT(topic,aggregate_id) DO NOTHING;
-- name: ResolvePush :one
SELECT d.token_cipher,n.kind,n.id,n.reference,COALESCE(announcement.template,'')::text AS template FROM app.push_devices d JOIN app.notifications n ON n.account_id=d.account_id JOIN app.identity_accounts a ON a.id=d.account_id JOIN app.identity_installations i ON i.id=d.installation_id JOIN app.identity_sessions s ON s.id=d.session_id JOIN app.notification_preferences p ON p.account_id=d.account_id LEFT JOIN app.delivery_announcements announcement ON announcement.id=n.reference
WHERE n.id=$1 AND d.installation_id=$2 AND d.revision=$3 AND d.updated_at>clock_timestamp()-interval '60 days' AND a.status='active' AND NOT i.restricted AND NOT s.revoked AND s.expires_at>clock_timestamp() AND CASE n.kind WHEN 'gift_received' THEN p.gift_received WHEN 'agency_decision' THEN p.agency_decision WHEN 'account_event' THEN p.account_event WHEN 'room_invitation' THEN p.room_invitation WHEN 'admin_announcement' THEN p.admin_announcement ELSE false END;
-- name: InvalidDevice :exec
DELETE FROM app.push_devices WHERE installation_id=$1 AND revision=$2;
-- name: CleanupDevices :exec
DELETE FROM app.push_devices WHERE installation_id IN (SELECT installation_id FROM app.push_devices WHERE expires_at<=clock_timestamp() ORDER BY expires_at,installation_id LIMIT 100);
-- name: PruneRevokedBindings :exec
DELETE FROM app.push_devices d WHERE d.account_id=$1 AND NOT EXISTS(SELECT 1 FROM app.identity_sessions s WHERE s.id=d.session_id AND NOT s.revoked AND s.expires_at>clock_timestamp());
-- name: NotificationHistory :many
SELECT n.id,n.kind,n.reference,n.created_at,COALESCE(a.template,'')::text AS template FROM app.notifications n LEFT JOIN app.delivery_announcements a ON a.id=n.reference WHERE n.account_id=$1 AND n.id>$2 ORDER BY n.id LIMIT $3;

-- name: CreateAnnouncement :exec
INSERT INTO app.delivery_announcements(id,template) VALUES($1,$2);

-- name: RetryDelivery :execrows
UPDATE app.delivery_outbox SET state='pending',attempts=0,lease_id='',lease_until=NULL,available_at=clock_timestamp(),error_code='',updated_at=clock_timestamp() WHERE id=$1 AND state='failed';

-- name: UnbindInactiveInstallation :exec
DELETE FROM app.push_devices d USING app.identity_sessions s,app.identity_accounts a WHERE d.session_id=s.id AND d.account_id=a.id AND d.installation_id=$1 AND (s.revoked OR s.expires_at<=clock_timestamp() OR a.status<>'active');
