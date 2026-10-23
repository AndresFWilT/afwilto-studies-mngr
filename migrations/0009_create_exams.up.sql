-- Spec 18 FR-1 / slice 02: an exam hangs off a subject. Deletion is logical;
-- the name is unique among the alive exams of a subject, compared trimmed and
-- case-insensitively (BR-E1). `version` guards edits against stale writers.
CREATE TABLE exams (
    id             uuid PRIMARY KEY,
    subject_id     uuid NOT NULL REFERENCES subjects(id),
    name           text NOT NULL,
    question_count integer NOT NULL,
    time_limit_min integer NOT NULL,
    pass_percent   integer NOT NULL,
    version        integer NOT NULL DEFAULT 1,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    deleted_at     timestamptz,
    CONSTRAINT exam_name_len     CHECK (char_length(name) BETWEEN 1 AND 120),
    CONSTRAINT exam_count_range  CHECK (question_count BETWEEN 1 AND 200),
    CONSTRAINT exam_time_range   CHECK (time_limit_min BETWEEN 1 AND 600),
    CONSTRAINT exam_pass_range   CHECK (pass_percent BETWEEN 1 AND 100),
    CONSTRAINT exam_version_pos  CHECK (version >= 1)
);

CREATE UNIQUE INDEX exams_subject_name_alive_ux
    ON exams (subject_id, lower(btrim(name))) WHERE deleted_at IS NULL;

CREATE INDEX exams_subject_alive_ix ON exams (subject_id) WHERE deleted_at IS NULL;

CREATE TRIGGER exams_no_delete BEFORE DELETE ON exams
    FOR EACH ROW EXECUTE FUNCTION forbid_hard_delete();
