-- name: LockSecurity :exec
SELECT pg_advisory_xact_lock(772341909);
-- name: Owners :many
SELECT account_id FROM app.access_owners ORDER BY account_id LIMIT 2;
-- name: Bootstrap :exec
INSERT INTO app.access_owners(account_id) VALUES($1);
-- name: GoogleLinked :one
SELECT EXISTS(SELECT 1 FROM app.identity_google WHERE account_id=$1);
-- name: Permissions :many
SELECT DISTINCT p.permission FROM app.access_assignments a JOIN app.access_role_permissions p ON p.role_id=a.role_id WHERE a.account_id=$1 ORDER BY p.permission;
-- name: Directory :many
SELECT a.id AS account_id,a.status,p.display_name,p.bio,p.avatar_url FROM app.identity_accounts a JOIN app.profiles p ON p.account_id=a.id WHERE a.id>$1 ORDER BY a.id LIMIT $2;
-- name: User :one
SELECT a.id AS account_id,a.status,p.display_name,p.bio,p.avatar_url FROM app.identity_accounts a JOIN app.profiles p ON p.account_id=a.id WHERE a.id=$1;
-- name: Private :one
SELECT a.contact_email,COALESCE(u.value,'')::text AS username,COALESCE(p.value,'')::text AS phone FROM app.identity_accounts a LEFT JOIN app.identity_identifiers u ON u.account_id=a.id AND u.kind='username' LEFT JOIN app.identity_identifiers p ON p.account_id=a.id AND p.kind='phone' WHERE a.id=$1;
-- name: Roles :many
SELECT r.id,r.name,COALESCE(array_agg(p.permission ORDER BY p.permission) FILTER(WHERE p.permission IS NOT NULL),'{}'::text[])::text[] AS permissions FROM app.access_roles r LEFT JOIN app.access_role_permissions p ON p.role_id=r.id GROUP BY r.id ORDER BY r.id LIMIT 101;
-- name: PutRole :exec
INSERT INTO app.access_roles(id,name) VALUES($1,$2) ON CONFLICT(id) DO UPDATE SET name=EXCLUDED.name;
-- name: DeletePermissions :exec
DELETE FROM app.access_role_permissions WHERE role_id=$1;
-- name: AddPermission :exec
INSERT INTO app.access_role_permissions(role_id,permission) VALUES($1,$2);
-- name: DeleteRole :one
DELETE FROM app.access_roles WHERE id=$1 RETURNING id;
-- name: Assignments :many
SELECT role_id FROM app.access_assignments WHERE account_id=$1 ORDER BY role_id LIMIT 101;
-- name: ClearAssignments :exec
DELETE FROM app.access_assignments WHERE account_id=$1;
-- name: Assign :exec
INSERT INTO app.access_assignments(account_id,role_id) VALUES($1,$2);
-- name: Audit :exec
INSERT INTO app.access_audit(id,actor,target,action,fields,reason,correlation) VALUES($1,$2,$3,$4,$5,$6,$7);

-- name: RoleExists :one
SELECT EXISTS(SELECT 1 FROM app.access_roles WHERE id=$1);

-- name: CapabilityPolicy :one
SELECT h.policy_id,h.revision,v.roles,EXISTS(SELECT 1 FROM app.live_global_bans b WHERE b.account_id=sqlc.arg(actor)::text AND b.active) AS actor_live_banned FROM app.access_policy_head h JOIN app.access_policy_versions v ON v.id=h.policy_id WHERE h.singleton;
-- name: CapabilityOverrides :many
SELECT action,allowed FROM app.access_overrides WHERE account_id=$1 AND room_id IN ('',sqlc.arg(room_id)::text) AND expires_at>clock_timestamp() ORDER BY action,allowed;
-- name: CapabilityRoomRole :one
SELECT r.owner_id,r.state AS room_state,r.visibility,COALESCE(CASE WHEN m.state='active' THEN m.role END,'')::text AS role,COALESCE(m.state,'')::text AS member_state,COALESCE(m.private_entry,false)::boolean AS private_entry FROM app.live_rooms r LEFT JOIN app.live_members m ON m.room_id=r.id AND m.account_id=$2 WHERE r.id=$1;
-- name: CapabilityPackages :many
SELECT DISTINCT c.action FROM app.package_entitlements e JOIN app.package_catalog p ON p.id=e.package_id JOIN app.package_versions v ON v.id=p.current_version JOIN app.access_capabilities c ON c.action=ANY(v.powers) AND c.package_safe WHERE e.account_id=$1 AND e.starts_at<=clock_timestamp() AND e.expires_at>clock_timestamp() AND v.active AND NOT EXISTS(SELECT 1 FROM app.package_revocations r WHERE r.entitlement_id=e.id) ORDER BY c.action;
-- name: InsertCapabilityPolicy :exec
INSERT INTO app.access_policy_versions(id,previous_id,roles) VALUES($1,$2,sqlc.arg(roles)::text::jsonb);
-- name: SwitchCapabilityPolicy :one
UPDATE app.access_policy_head SET policy_id=$1,revision=revision+1 WHERE singleton AND policy_id=$2 RETURNING revision;
-- name: PutOverride :exec
INSERT INTO app.access_overrides(account_id,room_id,action,allowed,expires_at) VALUES($1,$2,$3,$4,$5) ON CONFLICT(account_id,room_id,action) DO UPDATE SET allowed=EXCLUDED.allowed,expires_at=EXCLUDED.expires_at;
-- name: DeleteOverride :exec
DELETE FROM app.access_overrides WHERE account_id=$1 AND room_id=$2 AND action=$3;
-- name: BumpCapabilityRevision :exec
UPDATE app.access_policy_head SET revision=revision+1 WHERE singleton;
