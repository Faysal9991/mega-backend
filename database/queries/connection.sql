-- name: ConnectionIdentity :one
SELECT current_database()::text AS database_name,
       current_user::text AS role_name,
       has_schema_privilege('app', 'USAGE')::boolean AS schema_ready;
