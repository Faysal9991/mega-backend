-- name: Agency :one
SELECT id,name,status,created_at FROM app.agencies WHERE id=$1;
-- name: CreateAgency :one
INSERT INTO app.agencies(id,name,status) VALUES($1,$2,'pending') RETURNING id,name,status,created_at;
-- name: UpdateAgency :one
UPDATE app.agencies SET name=$2,status=$3 WHERE id=$1 RETURNING id,name,status,created_at;
-- name: Agencies :many
SELECT id,name,status,created_at FROM app.agencies WHERE id>$1 ORDER BY id LIMIT $2;
-- name: Manager :one
SELECT EXISTS(SELECT 1 FROM app.agency_managers WHERE agency_id=$1 AND account_id=$2);
-- name: AddManager :exec
INSERT INTO app.agency_managers(agency_id,account_id) VALUES($1,$2) ON CONFLICT DO NOTHING;
-- name: RemoveManager :exec
DELETE FROM app.agency_managers WHERE agency_id=$1 AND account_id=$2;
-- name: Application :one
SELECT id,agency_id,account_id,classification,state,created_at,decided_at FROM app.host_applications WHERE id=$1;
-- name: Apply :one
INSERT INTO app.host_applications(id,agency_id,account_id,classification,state) VALUES($1,$2,$3,$4,'pending') RETURNING id,agency_id,account_id,classification,state,created_at,decided_at;
-- name: Decide :one
UPDATE app.host_applications SET state=$2,decided_at=clock_timestamp() WHERE id=$1 AND state='pending' RETURNING id,agency_id,account_id,classification,state,created_at,decided_at;
-- name: Applications :many
SELECT id,agency_id,account_id,classification,state,created_at,decided_at FROM app.host_applications WHERE agency_id=$1 AND id>$2 ORDER BY id LIMIT $3;
-- name: Association :one
SELECT id,account_id,agency_id,classification,effective_from,effective_until FROM app.agency_host_associations WHERE account_id=$1 AND effective_until IS NULL AND effective_from<=clock_timestamp();
-- name: History :many
SELECT id,account_id,agency_id,classification,effective_from,effective_until FROM app.agency_host_associations WHERE account_id=$1 AND id>$2 ORDER BY id LIMIT $3;
-- name: Room :one
SELECT id,session_id,provider_sid,owner_id,kind,title,state,capacity,seats,created_at,closed_at FROM app.live_rooms WHERE id=$1;
-- name: SessionRoom :one
SELECT id,session_id,provider_sid,owner_id,kind,title,state,capacity,seats,created_at,closed_at FROM app.live_rooms WHERE session_id=$1;
-- name: Rooms :many
SELECT id,session_id,provider_sid,owner_id,kind,title,state,capacity,seats,created_at,closed_at FROM app.live_rooms WHERE id>$1 AND (sqlc.arg(admin)::boolean OR state='open') ORDER BY id LIMIT sqlc.arg(page_limit);
-- name: CreateRoom :one
INSERT INTO app.live_rooms(id,session_id,owner_id,kind,title,state,capacity,seats) VALUES($1,$2,$3,$4,$5,'opening',$6,$7) RETURNING id,session_id,provider_sid,owner_id,kind,title,state,capacity,seats,created_at,closed_at;
-- name: RoomState :exec
UPDATE app.live_rooms SET state=$2,closed_at=CASE WHEN $2='closed' THEN clock_timestamp() ELSE closed_at END WHERE id=$1;
-- name: Member :one
SELECT room_id,account_id,role,state,muted,seat,invited_seat,updated_at FROM app.live_members WHERE room_id=$1 AND account_id=$2;
-- name: Members :many
SELECT room_id,account_id,role,state,muted,seat,invited_seat,updated_at FROM app.live_members WHERE room_id=$1 AND state='active' ORDER BY account_id LIMIT 501;
-- name: JoinMember :one
INSERT INTO app.live_members(room_id,account_id,role,state) VALUES($1,$2,$3,'active') ON CONFLICT(room_id,account_id) DO UPDATE SET state='active',role=EXCLUDED.role,seat=NULL,invited_seat=NULL,muted=false,updated_at=clock_timestamp() WHERE app.live_members.state='left' RETURNING room_id,account_id,role,state,muted,seat,invited_seat,updated_at;
-- name: UpdateMember :one
UPDATE app.live_members SET role=$3,state=$4,muted=$5,seat=NULLIF(sqlc.arg(seat)::integer,0),invited_seat=NULLIF(sqlc.arg(invited_seat)::integer,0),updated_at=clock_timestamp() WHERE room_id=$1 AND account_id=$2 RETURNING room_id,account_id,role,state,muted,seat,invited_seat,updated_at;
-- name: CloseMembers :exec
UPDATE app.live_members SET state='left',seat=NULL,invited_seat=NULL,updated_at=clock_timestamp() WHERE room_id=$1 AND state='active';
-- name: Enqueue :exec
INSERT INTO app.live_operations(id,room_id,kind,target) VALUES($1,$2,$3,$4);
-- name: Pending :one
SELECT EXISTS(SELECT 1 FROM app.live_operations WHERE room_id=$1 AND state<>'done');
-- name: Claim :one
UPDATE app.live_operations SET state='running',lease_until=clock_timestamp()+interval '30 seconds',attempts=attempts+1 WHERE id=(SELECT id FROM app.live_operations WHERE app.live_operations.room_id=$1 AND state<>'done' ORDER BY sequence LIMIT 1) AND (state='pending' OR lease_until<clock_timestamp()) AND attempts<100 RETURNING id,room_id,kind,target;
-- name: Complete :exec
UPDATE app.live_operations SET state='done',lease_until=NULL WHERE id=$1 AND state='running';
-- name: Retry :exec
UPDATE app.live_operations SET state='pending',lease_until=NULL WHERE id=$1 AND state='running';
-- name: Existing :one
SELECT fingerprint,result FROM app.live_commands WHERE actor_id=$1 AND kind=$2 AND key=$3;
-- name: Save :exec
INSERT INTO app.live_commands(actor_id,kind,key,fingerprint,result) VALUES($1,$2,$3,$4,sqlc.arg(result)::text::jsonb);
-- name: Event :one
SELECT fingerprint FROM app.live_events WHERE id=$1;
-- name: RecordEvent :exec
INSERT INTO app.live_events(id,fingerprint,event_type,room_id,participant_sid,provider_at) VALUES($1,$2,$3,$4,$5,$6);
-- name: Presence :one
SELECT participant_sid,room_id,account_id,association_id,classification,started_at,ended_at,qualifying,valid_until FROM app.live_presence WHERE participant_sid=$1;
-- name: StartPresence :exec
INSERT INTO app.live_presence(participant_sid,room_id,account_id,association_id,classification,started_at,qualifying) VALUES(sqlc.arg(participant_sid),sqlc.arg(room_id),sqlc.arg(account_id),NULLIF(sqlc.arg(association_id)::text,''),sqlc.arg(classification),sqlc.arg(started_at),sqlc.arg(qualifying)) ON CONFLICT DO NOTHING;
-- name: EndPresence :exec
UPDATE app.live_presence SET ended_at=$2 WHERE participant_sid=$1 AND ended_at IS NULL;
-- name: EndTombstone :exec
INSERT INTO app.live_presence(participant_sid,room_id,account_id,classification,ended_at,qualifying) VALUES($1,$2,$3,'',$4,false) ON CONFLICT DO NOTHING;
-- name: InvalidatePresence :exec
UPDATE app.live_presence SET valid_until=LEAST(COALESCE(valid_until,clock_timestamp()),clock_timestamp()) WHERE room_id=$1 AND account_id=$2 AND ended_at IS NULL;
-- name: InvalidateRoomPresence :exec
UPDATE app.live_presence SET valid_until=LEAST(COALESCE(valid_until,clock_timestamp()),clock_timestamp()) WHERE room_id=$1 AND ended_at IS NULL;
-- name: Activity :many
WITH current_context AS (SELECT agency_id,started_at FROM app.agency_activity_contexts WHERE account_id=$1 ORDER BY revision DESC LIMIT 1)
SELECT p.participant_sid,p.account_id,p.started_at,LEAST(p.ended_at,COALESCE(p.valid_until,p.ended_at),COALESCE(a.effective_until,p.ended_at),COALESCE(r.restricted_at,p.ended_at))::timestamptz AS ended_at
FROM app.live_presence p JOIN app.agency_host_associations a ON a.id=p.association_id
JOIN current_context c ON c.agency_id=a.agency_id AND p.started_at>=c.started_at
LEFT JOIN LATERAL (SELECT restricted_at FROM app.live_account_restrictions WHERE account_id=p.account_id AND restricted_at>=p.started_at AND restricted_at<=p.ended_at ORDER BY restricted_at,id LIMIT 1) r ON true
WHERE p.account_id=$1 AND p.qualifying AND p.ended_at IS NOT NULL AND p.started_at<$3 AND p.ended_at>$2 AND LEAST(p.ended_at,COALESCE(p.valid_until,p.ended_at),COALESCE(a.effective_until,p.ended_at),COALESCE(r.restricted_at,p.ended_at)) <= $3
ORDER BY p.ended_at,p.participant_sid LIMIT 10001;
-- name: OpenPresence :many
SELECT participant_sid,room_id,account_id FROM app.live_presence WHERE room_id=$1 AND ended_at IS NULL ORDER BY participant_sid LIMIT 501;
-- name: Clock :one
SELECT clock_timestamp()::timestamptz AS now;
-- name: Ready :one
SELECT to_regclass('app.live_presence') IS NOT NULL AND to_regclass('app.agencies') IS NOT NULL AND to_regclass('app.live_policies') IS NOT NULL AND to_regclass('app.live_account_restrictions') IS NOT NULL AND to_regclass('app.agency_activity_contexts') IS NOT NULL AND to_regclass('app.live_presence_account_sid') IS NOT NULL AND EXISTS(SELECT 1 FROM pg_catalog.pg_trigger WHERE tgrelid=to_regclass('app.live_presence') AND tgname='live_presence_integrity' AND tgenabled<>'D');
-- name: AbandonPresence :exec
UPDATE app.live_presence SET ended_at=started_at WHERE participant_sid=$1 AND ended_at IS NULL;
-- name: ActiveRooms :many
SELECT room_id FROM app.live_members WHERE account_id=$1 AND state='active' ORDER BY room_id LIMIT 501;
-- name: AgencyHosts :many
SELECT account_id FROM app.agency_host_associations WHERE agency_id=$1 AND effective_until IS NULL ORDER BY account_id LIMIT 501;
-- name: InspectActivity :many
SELECT p.participant_sid,p.room_id,p.account_id,COALESCE(p.association_id,'')::text AS association_id,p.classification,p.started_at,p.ended_at,p.valid_until,r.restricted_at FROM app.live_presence p LEFT JOIN LATERAL (SELECT restricted_at FROM app.live_account_restrictions WHERE account_id=p.account_id AND restricted_at>=p.started_at AND (p.ended_at IS NULL OR restricted_at<=p.ended_at) ORDER BY restricted_at,id LIMIT 1) r ON true WHERE p.account_id=$1 AND p.participant_sid>$2 ORDER BY p.participant_sid LIMIT $3;
-- name: AffectedMembers :many
SELECT m.room_id,m.account_id FROM app.live_members m JOIN app.agency_host_associations a ON a.account_id=m.account_id AND a.effective_until IS NULL WHERE (a.account_id=sqlc.arg(account_id)::text OR a.agency_id=sqlc.arg(agency_id)::text) AND m.state='active' ORDER BY m.room_id,m.account_id LIMIT 501;
-- name: EnqueueMembers :copyfrom
INSERT INTO app.live_operations(id,room_id,kind,target) VALUES($1,$2,$3,$4);
-- name: InvalidateMembers :exec
UPDATE app.live_presence SET valid_until=LEAST(COALESCE(valid_until,clock_timestamp()),clock_timestamp()) WHERE account_id=ANY($1::text[]) AND ended_at IS NULL;
-- name: AccountActive :one
SELECT status='active' FROM app.identity_accounts WHERE id=$1;

-- name: BindProviderSID :one
UPDATE app.live_rooms SET provider_sid=$2 WHERE id=$1 AND (provider_sid='' OR provider_sid=$2) RETURNING provider_sid;

-- name: LatestLivePolicy :one
SELECT id,config,created_at FROM app.live_policies ORDER BY revision DESC LIMIT 1;
-- name: PublishLivePolicy :one
INSERT INTO app.live_policies(id,previous_id,actor_id,config) VALUES($1,$2,$3,sqlc.arg(config)::text::jsonb) RETURNING created_at;
-- name: AllAffectedMembers :many
SELECT room_id,account_id FROM app.live_members WHERE state='active' AND (role IN ('host','co_host') OR seat IS NOT NULL) ORDER BY room_id,account_id LIMIT 501;
-- name: HistoricalAssociation :one
SELECT id,account_id,agency_id,classification,effective_from,effective_until FROM app.agency_host_associations WHERE id=$1;
-- name: OperationPreview :many
SELECT id,kind,target,state,attempts,lease_until FROM app.live_operations WHERE room_id=$1 AND state<>'done' ORDER BY sequence LIMIT 51;
-- name: ResetExhausted :exec
UPDATE app.live_operations SET attempts=0,state='pending',lease_until=NULL WHERE id=(SELECT id FROM app.live_operations WHERE app.live_operations.room_id=$1 AND attempts>=100 AND (state='pending' OR lease_until<clock_timestamp()) ORDER BY sequence LIMIT 1);
-- name: RecordRestriction :exec
INSERT INTO app.live_account_restrictions(id,account_id) VALUES($1,$2);
-- name: LeaveRestrictedRooms :exec
UPDATE app.live_members SET state='left',seat=NULL,invited_seat=NULL,updated_at=clock_timestamp() WHERE account_id=sqlc.arg(account_id) AND room_id=ANY(sqlc.arg(room_ids)::text[]) AND state='active';
-- name: RestrictionHistory :many
SELECT id,restricted_at FROM app.live_account_restrictions WHERE account_id=$1 ORDER BY restricted_at,id LIMIT 100;

-- name: AbandonPresences :exec
UPDATE app.live_presence SET ended_at=started_at WHERE participant_sid=ANY($1::text[]) AND ended_at IS NULL;

-- name: DesiredParticipant :one
SELECT m.room_id,m.account_id,m.role,m.state,m.muted,m.seat,m.invited_seat,m.updated_at,
 COALESCE(a.classification,'')::text AS classification,
 COALESCE(g.status='active',false)::boolean AS agency_active,
 (i.status='active')::boolean AS account_active
FROM app.live_members m JOIN app.identity_accounts i ON i.id=m.account_id
LEFT JOIN app.agency_host_associations a ON a.account_id=m.account_id AND a.effective_until IS NULL AND a.effective_from<=clock_timestamp()
LEFT JOIN app.agencies g ON g.id=a.agency_id
WHERE m.room_id=$1 AND m.account_id=$2;
