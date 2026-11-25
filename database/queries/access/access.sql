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
