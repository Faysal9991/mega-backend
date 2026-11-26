-- +goose Up
-- +goose StatementBegin
DO $$
BEGIN
 PERFORM pg_advisory_xact_lock(772341909);
 -- Correct only the original, never user-published seed. Preserve immutable
 -- history and every explicitly configured policy/role assignment.
 IF EXISTS(SELECT 1 FROM app.access_policy_head WHERE singleton AND policy_id='00000000000000000000000000000001') THEN
  INSERT INTO app.access_policy_versions(id,previous_id,roles)
  SELECT '00000000000000000000000000000012',id,jsonb_set(roles,'{admin}','["room.participant.kick","room.participant.mute","room.participant.seat_remove","room.seat.self_leave"]'::jsonb)
  FROM app.access_policy_versions WHERE id='00000000000000000000000000000001';
  UPDATE app.access_policy_head SET policy_id='00000000000000000000000000000012',revision=revision+1 WHERE singleton;
 END IF;
END $$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$
BEGIN
 PERFORM pg_advisory_xact_lock(772341909);
 IF EXISTS(SELECT 1 FROM app.access_policy_versions WHERE previous_id='00000000000000000000000000000012') THEN
  RAISE EXCEPTION 'default-policy rollback requires reviewed retained-policy recovery';
 END IF;
 UPDATE app.access_policy_head SET policy_id='00000000000000000000000000000001',revision=revision+1
 WHERE singleton AND policy_id='00000000000000000000000000000012';
 DELETE FROM app.access_policy_versions WHERE id='00000000000000000000000000000012';
END $$;
-- +goose StatementEnd
