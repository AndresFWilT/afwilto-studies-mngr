-- Spec 16 FR-0: deletion is always logical (deleted_at). Every table gets a
-- BEFORE DELETE trigger calling this function, so a hard DELETE fails even
-- from a hand-written psql session. TRUNCATE is statement-level and is
-- deliberately left allowed (test resets, admin wipes).
CREATE FUNCTION forbid_hard_delete() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'hard delete is forbidden on %; set deleted_at instead', TG_TABLE_NAME
        USING ERRCODE = 'restrict_violation';
END
$$;
