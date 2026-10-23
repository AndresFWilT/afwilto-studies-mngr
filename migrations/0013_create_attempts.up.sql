-- Spec 18 slice 06a: attempts and their frozen items. An attempt keeps the
-- session config and the seed it was drawn with; every item keeps a snapshot
-- of its question as displayed (options shuffled and relabelled, with the
-- answer key), so later edits or deletes never reach a past attempt (AC-8).
-- Neither table is ever hard-deleted (forbid_hard_delete, Spec 16 FR-0).
--
-- suggested_minutes and pass_percent are frozen at the start (slice 06a
-- amendment 2026-10-23): the GET of an attempt and its scoring must not change
-- when the exam is edited later.
CREATE TABLE attempts (
    id                uuid PRIMARY KEY,
    exam_id           uuid NOT NULL REFERENCES exams(id),
    config            jsonb NOT NULL,
    status            text NOT NULL DEFAULT 'in_progress',
    -- the uint64 seed of the draw, stored as its two's-complement bit pattern
    seed              bigint NOT NULL,
    started_at        timestamptz NOT NULL,
    deadline_at       timestamptz,
    submitted_at      timestamptz,
    score_percent     numeric(5,2),
    passed            boolean,
    suggested_minutes integer NOT NULL,
    pass_percent      integer NOT NULL,
    CONSTRAINT attempt_status       CHECK (status IN ('in_progress', 'submitted', 'expired')),
    CONSTRAINT attempt_config_obj   CHECK (jsonb_typeof(config) = 'object'),
    CONSTRAINT attempt_deadline     CHECK (deadline_at IS NULL OR deadline_at > started_at),
    CONSTRAINT attempt_score_range  CHECK (score_percent IS NULL OR score_percent BETWEEN 0 AND 100),
    CONSTRAINT attempt_suggested    CHECK (suggested_minutes >= 0),
    CONSTRAINT attempt_pass_range   CHECK (pass_percent BETWEEN 1 AND 100)
);

-- BR-A1: at most one open attempt per exam, enforced where races cannot get
-- past it.
CREATE UNIQUE INDEX attempts_exam_in_progress_ux ON attempts (exam_id) WHERE status = 'in_progress';
CREATE INDEX attempts_exam_started_ix ON attempts (exam_id, started_at DESC);

CREATE TRIGGER attempts_no_delete BEFORE DELETE ON attempts
    FOR EACH ROW EXECUTE FUNCTION forbid_hard_delete();

CREATE TABLE attempt_items (
    attempt_id  uuid NOT NULL REFERENCES attempts(id),
    position    integer NOT NULL,
    question_id uuid NOT NULL REFERENCES questions(id),
    snapshot    jsonb NOT NULL,
    selected    text[] NOT NULL DEFAULT '{}',
    flagged     boolean NOT NULL DEFAULT false,
    answered_at timestamptz,
    -- set by slice 06b once the answer key of a "each" feedback item was shown
    revealed    boolean NOT NULL DEFAULT false,
    correct     boolean,
    PRIMARY KEY (attempt_id, position),
    CONSTRAINT attempt_item_position CHECK (position >= 0),
    CONSTRAINT attempt_item_snapshot CHECK (jsonb_typeof(snapshot) = 'object'),
    CONSTRAINT attempt_item_labels   CHECK (selected <@ ARRAY['A', 'B', 'C', 'D', 'E', 'F']::text[])
);

CREATE TRIGGER attempt_items_no_delete BEFORE DELETE ON attempt_items
    FOR EACH ROW EXECUTE FUNCTION forbid_hard_delete();
