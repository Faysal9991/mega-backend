-- name: Initialize :exec
INSERT INTO app.profiles(account_id,display_name,avatar_url) VALUES($1,$2,$3);
-- name: Read :one
SELECT account_id,display_name,bio,avatar_url FROM app.profiles WHERE account_id=$1;
-- name: Update :one
UPDATE app.profiles SET display_name=CASE WHEN sqlc.arg(set_name)::boolean THEN sqlc.arg(display_name)::text ELSE display_name END,bio=CASE WHEN sqlc.arg(set_bio)::boolean THEN sqlc.arg(bio)::text ELSE bio END WHERE account_id=sqlc.arg(account_id) RETURNING account_id,display_name,bio,avatar_url;
