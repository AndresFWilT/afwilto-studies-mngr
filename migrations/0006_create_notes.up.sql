-- Spec 16 FR-3: Markdown notes that belong to a subject and have a kind.
-- Deletion is logical; title uniqueness applies among the alive notes of a
-- subject. `version` guards edits against stale writers (BR-NOTE3).
CREATE TABLE notes (
    id          uuid PRIMARY KEY,
    subject_id  uuid NOT NULL REFERENCES subjects(id),
    kind_id     uuid NOT NULL REFERENCES note_kinds(id),
    title       text NOT NULL,
    content     text NOT NULL DEFAULT '',
    slab_links  jsonb NOT NULL DEFAULT '[]',
    version     integer NOT NULL DEFAULT 1,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    deleted_at  timestamptz,
    CONSTRAINT note_title_len   CHECK (char_length(title) BETWEEN 1 AND 200),
    CONSTRAINT note_content_len CHECK (char_length(content) <= 200000),
    CONSTRAINT note_links_array CHECK (jsonb_typeof(slab_links) = 'array'),
    CONSTRAINT note_version_pos CHECK (version >= 1)
);

CREATE UNIQUE INDEX notes_subject_title_alive_ux
    ON notes (subject_id, lower(title)) WHERE deleted_at IS NULL;
CREATE INDEX notes_subject_alive_ix ON notes (subject_id) WHERE deleted_at IS NULL;
CREATE INDEX notes_kind_alive_ix    ON notes (kind_id)    WHERE deleted_at IS NULL;

CREATE TRIGGER notes_no_delete BEFORE DELETE ON notes
    FOR EACH ROW EXECUTE FUNCTION forbid_hard_delete();

-- Slice 05, S05-12: the note/subject/kind relations are enforced by the
-- database. All three guards take one advisory lock, so a note write and a
-- subject or kind delete serialize and cannot both succeed.
CREATE FUNCTION guard_note_refs() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.deleted_at IS NOT NULL THEN
        RETURN NEW;
    END IF;
    IF TG_OP = 'INSERT'
       OR NEW.subject_id IS DISTINCT FROM OLD.subject_id
       OR NEW.kind_id IS DISTINCT FROM OLD.kind_id
       OR OLD.deleted_at IS NOT NULL THEN -- placement change or manual restore
        PERFORM pg_advisory_xact_lock(hashtext('studies.notes.refs'));
        IF NOT EXISTS (SELECT 1 FROM subjects WHERE id = NEW.subject_id AND deleted_at IS NULL) THEN
            RAISE EXCEPTION 'subject % does not exist', NEW.subject_id
                USING ERRCODE = 'check_violation', CONSTRAINT = 'note_subject_not_found';
        END IF;
        IF NOT EXISTS (SELECT 1 FROM note_kinds WHERE id = NEW.kind_id AND deleted_at IS NULL) THEN
            RAISE EXCEPTION 'note kind % does not exist', NEW.kind_id
                USING ERRCODE = 'check_violation', CONSTRAINT = 'note_kind_not_found';
        END IF;
    END IF;
    RETURN NEW;
END $$;

CREATE TRIGGER notes_guard_refs BEFORE INSERT OR UPDATE ON notes
    FOR EACH ROW EXECUTE FUNCTION guard_note_refs();

CREATE FUNCTION guard_subject_notes() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.deleted_at IS NULL AND NEW.deleted_at IS NOT NULL THEN
        PERFORM pg_advisory_xact_lock(hashtext('studies.notes.refs'));
        IF EXISTS (SELECT 1 FROM notes WHERE subject_id = NEW.id AND deleted_at IS NULL) THEN
            RAISE EXCEPTION 'subject % still has alive notes', NEW.id
                USING ERRCODE = 'check_violation', CONSTRAINT = 'subject_in_use';
        END IF;
    END IF;
    RETURN NEW;
END $$;

CREATE TRIGGER subjects_guard_notes BEFORE UPDATE OF deleted_at ON subjects
    FOR EACH ROW EXECUTE FUNCTION guard_subject_notes();

CREATE FUNCTION guard_kind_notes() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.deleted_at IS NULL AND NEW.deleted_at IS NOT NULL THEN
        PERFORM pg_advisory_xact_lock(hashtext('studies.notes.refs'));
        IF EXISTS (SELECT 1 FROM notes WHERE kind_id = NEW.id AND deleted_at IS NULL) THEN
            RAISE EXCEPTION 'note kind % is still used by alive notes', NEW.id
                USING ERRCODE = 'check_violation', CONSTRAINT = 'kind_in_use';
        END IF;
    END IF;
    RETURN NEW;
END $$;

CREATE TRIGGER note_kinds_guard_notes BEFORE UPDATE OF deleted_at ON note_kinds
    FOR EACH ROW EXECUTE FUNCTION guard_kind_notes();
