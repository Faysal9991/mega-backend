-- name: EnsureInstallation :one
INSERT INTO app.identity_installations(id) VALUES ($1) ON CONFLICT(id) DO UPDATE SET id=EXCLUDED.id RETURNING restricted;
-- name: LockSubject :exec
SELECT pg_advisory_xact_lock(hashtextextended('google:' || $1::text,0));
-- name: GoogleAccount :one
SELECT a.id,a.status,p.display_name,a.contact_email,p.avatar_url FROM app.identity_accounts a JOIN app.identity_google g ON g.account_id=a.id JOIN app.profiles p ON p.account_id=a.id WHERE g.provider='google' AND g.subject=$1;
-- name: CreateAccount :exec
INSERT INTO app.identity_accounts(id,contact_email) VALUES($1,$2);
-- name: LinkGoogle :exec
INSERT INTO app.identity_google(subject,account_id) VALUES($1,$2);
-- name: LockAccount :one
SELECT a.id,a.status,p.display_name,a.contact_email,p.avatar_url FROM app.identity_accounts a JOIN app.profiles p ON p.account_id=a.id WHERE a.id=$1 FOR UPDATE OF a;
-- name: Password :one
SELECT credential FROM app.identity_passwords WHERE account_id=$1;
-- name: PutPassword :exec
INSERT INTO app.identity_passwords(account_id,credential) VALUES($1,$2) ON CONFLICT(account_id) DO UPDATE SET credential=EXCLUDED.credential;
-- name: FindIdentifier :one
SELECT account_id FROM app.identity_identifiers WHERE kind=$1 AND value=$2;
-- name: Identifiers :many
SELECT kind,value FROM app.identity_identifiers WHERE account_id=$1 ORDER BY kind;
-- name: PutIdentifier :exec
INSERT INTO app.identity_identifiers(account_id,kind,value) VALUES($1,$2,$3) ON CONFLICT(account_id,kind) DO UPDATE SET value=EXCLUDED.value;
-- name: CreateChallenge :exec
INSERT INTO app.identity_challenges(token_hash,installation_id,purpose,expires_at) VALUES($1,$2,$3,clock_timestamp()+interval '5 minutes');
-- name: Challenge :one
SELECT token_hash,installation_id,purpose,created_at,expires_at FROM app.identity_challenges WHERE token_hash=$1 AND NOT consumed AND expires_at>clock_timestamp();
-- name: ConsumeChallenge :one
UPDATE app.identity_challenges SET consumed=true WHERE token_hash=$1 AND NOT consumed AND expires_at>clock_timestamp() RETURNING token_hash;
-- name: CreateProof :exec
INSERT INTO app.identity_proofs(token_hash,account_id,installation_id,purpose,expires_at) VALUES($1,$2,$3,$4,clock_timestamp()+interval '5 minutes');
-- name: Proof :one
SELECT account_id,installation_id,purpose FROM app.identity_proofs WHERE token_hash=$1 AND NOT consumed AND expires_at>clock_timestamp();
-- name: ConsumeProof :one
UPDATE app.identity_proofs SET consumed=true WHERE token_hash=$1 AND account_id=$2 AND installation_id=$3 AND purpose=$4 AND NOT consumed AND expires_at>clock_timestamp() RETURNING token_hash;
-- name: CreateSession :exec
INSERT INTO app.identity_sessions(id,account_id,installation_id,access_hash,access_expires_at,expires_at) VALUES($1,$2,$3,$4,clock_timestamp()+interval '15 minutes',clock_timestamp()+interval '30 days');
-- name: PruneSessions :exec
UPDATE app.identity_sessions SET revoked=true WHERE id IN (SELECT id FROM app.identity_sessions WHERE identity_sessions.account_id=$1 AND NOT revoked AND expires_at>clock_timestamp() ORDER BY authenticated_at DESC,id DESC OFFSET 4);
-- name: AddRefresh :exec
INSERT INTO app.identity_refresh_tokens(token_hash,session_id) VALUES($1,$2);
-- name: RefreshSession :one
SELECT s.id,s.account_id,s.installation_id,s.revoked,s.expires_at,t.consumed FROM app.identity_refresh_tokens t JOIN app.identity_sessions s ON s.id=t.session_id WHERE t.token_hash=$1;
-- name: ConsumeRefresh :one
UPDATE app.identity_refresh_tokens SET consumed=true WHERE token_hash=$1 AND NOT consumed RETURNING token_hash;
-- name: RotateSession :exec
UPDATE app.identity_sessions SET access_hash=$2,access_expires_at=LEAST(clock_timestamp()+interval '15 minutes',expires_at) WHERE id=$1;
-- name: Access :one
SELECT s.id AS session_id,a.id AS account_id,p.display_name,a.contact_email,p.avatar_url FROM app.identity_sessions s JOIN app.identity_accounts a ON a.id=s.account_id JOIN app.identity_installations i ON i.id=s.installation_id JOIN app.profiles p ON p.account_id=a.id WHERE s.access_hash=$1 AND s.installation_id=$2 AND NOT s.revoked AND s.access_expires_at>clock_timestamp() AND s.expires_at>clock_timestamp() AND a.status='active' AND NOT i.restricted;
-- name: RevokeSession :exec
UPDATE app.identity_sessions SET revoked=true WHERE id=$1;
-- name: RevokeAll :exec
UPDATE app.identity_sessions SET revoked=true WHERE account_id=$1;
-- name: RestrictAccount :exec
UPDATE app.identity_accounts SET status='restricted' WHERE id=$1;
-- name: RestrictInstallation :exec
UPDATE app.identity_installations SET restricted=true WHERE id=$1;
-- name: Rate :one
INSERT INTO app.identity_rate_limits(scope_hash,window_at,attempts) VALUES($1,date_trunc('minute',clock_timestamp()),1) ON CONFLICT(scope_hash) DO UPDATE SET window_at=date_trunc('minute',clock_timestamp()),attempts=CASE WHEN identity_rate_limits.window_at=date_trunc('minute',clock_timestamp()) THEN identity_rate_limits.attempts+1 ELSE 1 END RETURNING attempts;
-- name: CleanupRates :exec
DELETE FROM app.identity_rate_limits WHERE scope_hash IN (SELECT scope_hash FROM app.identity_rate_limits WHERE window_at<statement_timestamp()-interval '1 day' LIMIT 100);
-- name: CleanupChallenges :exec
DELETE FROM app.identity_challenges WHERE token_hash IN (SELECT token_hash FROM app.identity_challenges WHERE expires_at<statement_timestamp() LIMIT 100);
-- name: CleanupProofs :exec
DELETE FROM app.identity_proofs WHERE token_hash IN (SELECT token_hash FROM app.identity_proofs WHERE expires_at<statement_timestamp() LIMIT 100);
-- name: SchemaReady :one
SELECT count(*)=16 AS ready FROM unnest(ARRAY['app.identity_accounts','app.identity_google','app.identity_passwords','app.identity_identifiers','app.identity_installations','app.identity_sessions','app.identity_refresh_tokens','app.identity_challenges','app.identity_proofs','app.identity_rate_limits','app.profiles','app.access_owners','app.access_roles','app.access_role_permissions','app.access_assignments','app.access_audit']) AS names(name) WHERE to_regclass(name) IS NOT NULL;
-- name: CleanupRefreshes :exec
DELETE FROM app.identity_refresh_tokens WHERE token_hash IN (SELECT t.token_hash FROM app.identity_refresh_tokens t JOIN app.identity_sessions s ON s.id=t.session_id WHERE s.expires_at<statement_timestamp() LIMIT 100);
-- name: CleanupSessions :exec
DELETE FROM app.identity_sessions WHERE id IN (SELECT s.id FROM app.identity_sessions s WHERE s.expires_at<statement_timestamp() AND NOT EXISTS (SELECT 1 FROM app.identity_refresh_tokens t WHERE t.session_id=s.id) LIMIT 100);

-- name: SetRestriction :exec
UPDATE app.identity_accounts SET status=CASE WHEN $2::boolean THEN 'restricted' ELSE 'active' END WHERE id=$1;
