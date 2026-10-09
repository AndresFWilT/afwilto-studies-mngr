-- Spec 16 FR-5: tags are created implicitly and shared across subjects;
-- note_tags links them to notes. Unlinking is logical (deleted_at) and
-- re-adding a tag revives the same row, so no row is ever removed.
CREATE TABLE tags (
    id   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text NOT NULL UNIQUE,
    CONSTRAINT tag_fmt CHECK (name ~ '^[a-z0-9]+(-[a-z0-9]+)*$' AND char_length(name) <= 40)
);

CREATE TABLE note_tags (
    note_id    uuid NOT NULL REFERENCES notes(id),
    tag_id     uuid NOT NULL REFERENCES tags(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz,
    PRIMARY KEY (note_id, tag_id)
);

CREATE INDEX note_tags_tag_alive_ix ON note_tags (tag_id) WHERE deleted_at IS NULL;

CREATE TRIGGER tags_no_delete BEFORE DELETE ON tags
    FOR EACH ROW EXECUTE FUNCTION forbid_hard_delete();
CREATE TRIGGER note_tags_no_delete BEFORE DELETE ON note_tags
    FOR EACH ROW EXECUTE FUNCTION forbid_hard_delete();
