-- Spec 16 FR-7: note kinds as a managed resource. Deletion is logical;
-- uniqueness applies among alive kinds only.
CREATE TABLE note_kinds (
    id         uuid PRIMARY KEY,
    name       text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz,
    CONSTRAINT kind_name_len CHECK (char_length(name) BETWEEN 1 AND 40)
);

CREATE UNIQUE INDEX note_kinds_name_alive_ux ON note_kinds (lower(name)) WHERE deleted_at IS NULL;

CREATE TRIGGER note_kinds_no_delete BEFORE DELETE ON note_kinds
    FOR EACH ROW EXECUTE FUNCTION forbid_hard_delete();

INSERT INTO note_kinds (id, name) VALUES
    ('00000000-0000-7000-8000-000000000001', 'Summary'),
    ('00000000-0000-7000-8000-000000000002', 'Cheat sheet'),
    ('00000000-0000-7000-8000-000000000003', 'Notes');
