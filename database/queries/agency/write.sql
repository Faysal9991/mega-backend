-- name: CloseAt :exec
UPDATE app.agency_host_associations SET effective_until=$2 WHERE account_id=$1 AND effective_until IS NULL;
-- name: InsertAt :exec
INSERT INTO app.agency_host_associations(id,account_id,agency_id,classification,effective_from,effective_until) VALUES($1,$2,$3,$4,$5,$6);
-- name: CloseNow :exec
UPDATE app.agency_host_associations SET effective_until=clock_timestamp() WHERE account_id=$1 AND effective_until IS NULL;
-- name: InsertNow :one
INSERT INTO app.agency_host_associations(id,account_id,agency_id,classification,effective_from) VALUES($1,$2,$3,$4,clock_timestamp()) RETURNING effective_from;

-- name: CurrentContext :one
SELECT agency_id,started_at FROM app.agency_activity_contexts WHERE account_id=$1 ORDER BY revision DESC LIMIT 1;
-- name: RecordContext :exec
INSERT INTO app.agency_activity_contexts(id,account_id,agency_id,started_at) VALUES(sqlc.arg(id),sqlc.arg(account_id),NULLIF(sqlc.arg(agency_id)::text,''),sqlc.arg(started_at));
-- name: Clock :one
SELECT clock_timestamp()::timestamptz AS now;
