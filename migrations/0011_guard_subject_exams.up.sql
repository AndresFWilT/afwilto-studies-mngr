-- Spec 18 slice 02 amendment (phase-1 review H1): a subject delete and an exam
-- create must not both succeed. Both guards take one advisory lock, as Spec 16's
-- guard_note_refs / guard_subject_notes do for notes, so the two writes
-- serialize and the second one sees the first one's outcome.
CREATE FUNCTION guard_exam_subject() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.deleted_at IS NOT NULL THEN
        RETURN NEW;
    END IF;
    IF TG_OP = 'INSERT'
       OR NEW.subject_id IS DISTINCT FROM OLD.subject_id
       OR OLD.deleted_at IS NOT NULL THEN -- subject change or manual restore
        PERFORM pg_advisory_xact_lock(hashtext('studies.exams.refs'));
        IF NOT EXISTS (SELECT 1 FROM subjects WHERE id = NEW.subject_id AND deleted_at IS NULL) THEN
            RAISE EXCEPTION 'subject % does not exist', NEW.subject_id
                USING ERRCODE = 'check_violation', CONSTRAINT = 'exam_subject_not_found';
        END IF;
    END IF;
    RETURN NEW;
END $$;

CREATE TRIGGER exams_guard_subject BEFORE INSERT OR UPDATE ON exams
    FOR EACH ROW EXECUTE FUNCTION guard_exam_subject();

CREATE FUNCTION guard_subject_exams() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.deleted_at IS NULL AND NEW.deleted_at IS NOT NULL THEN
        PERFORM pg_advisory_xact_lock(hashtext('studies.exams.refs'));
        IF EXISTS (SELECT 1 FROM exams WHERE subject_id = NEW.id AND deleted_at IS NULL) THEN
            RAISE EXCEPTION 'subject % still has alive exams', NEW.id
                USING ERRCODE = 'check_violation', CONSTRAINT = 'subject_in_use';
        END IF;
    END IF;
    RETURN NEW;
END $$;

CREATE TRIGGER subjects_guard_exams BEFORE UPDATE OF deleted_at ON subjects
    FOR EACH ROW EXECUTE FUNCTION guard_subject_exams();
