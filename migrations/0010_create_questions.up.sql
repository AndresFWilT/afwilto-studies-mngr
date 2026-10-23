-- Spec 18 slice 03: the question bank. Every table is logical-delete only
-- (forbid_hard_delete, Spec 16 FR-0). `ord` is an identity column that gives
-- creation order a total order inside one transaction, where every row of an
-- import shares the same timestamp; category order of first appearance (the
-- tie-break of the inferred weights) is read from it.

-- Up to 2 classifications per exam (BR-C1, enforced by the import under the
-- exam row lock). position 0 is the primary classification; every other one
-- is 1. While an exam has no primary (FR-3.1 rule 4) all of its alive
-- classifications are 1, so only the primary is unique by position.
CREATE TABLE exam_classifications (
    id          uuid PRIMARY KEY,
    exam_id     uuid NOT NULL REFERENCES exams(id),
    name        text NOT NULL,
    fold_name   text NOT NULL,
    position    smallint NOT NULL,
    weight_mode text NOT NULL DEFAULT 'inferred',
    ord         bigint GENERATED ALWAYS AS IDENTITY,
    created_at  timestamptz NOT NULL DEFAULT now(),
    deleted_at  timestamptz,
    CONSTRAINT classification_name_len CHECK (char_length(name) BETWEEN 1 AND 40),
    CONSTRAINT classification_position CHECK (position IN (0, 1)),
    CONSTRAINT classification_mode     CHECK (weight_mode IN ('inferred', 'manual'))
);

CREATE UNIQUE INDEX exam_classifications_fold_alive_ux
    ON exam_classifications (exam_id, fold_name) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX exam_classifications_primary_alive_ux
    ON exam_classifications (exam_id) WHERE position = 0 AND deleted_at IS NULL;

CREATE TRIGGER exam_classifications_no_delete BEFORE DELETE ON exam_classifications
    FOR EACH ROW EXECUTE FUNCTION forbid_hard_delete();

CREATE TABLE exam_categories (
    id                uuid PRIMARY KEY,
    classification_id uuid NOT NULL REFERENCES exam_classifications(id),
    key               text NOT NULL,
    name              text NOT NULL,
    fold_name         text NOT NULL,
    weight_percent    integer NOT NULL DEFAULT 0,
    pace_questions    integer,
    pace_minutes      integer,
    ord               bigint GENERATED ALWAYS AS IDENTITY,
    created_at        timestamptz NOT NULL DEFAULT now(),
    deleted_at        timestamptz,
    CONSTRAINT category_name_len CHECK (char_length(name) BETWEEN 1 AND 80),
    CONSTRAINT category_weight   CHECK (weight_percent BETWEEN 0 AND 100),
    CONSTRAINT category_pace_pair CHECK ((pace_questions IS NULL) = (pace_minutes IS NULL)),
    CONSTRAINT category_pace_pos  CHECK (pace_questions IS NULL OR (pace_questions >= 1 AND pace_minutes >= 1))
);

CREATE UNIQUE INDEX exam_categories_fold_alive_ux
    ON exam_categories (classification_id, fold_name) WHERE deleted_at IS NULL;

CREATE TRIGGER exam_categories_no_delete BEFORE DELETE ON exam_categories
    FOR EACH ROW EXECUTE FUNCTION forbid_hard_delete();

CREATE TABLE questions (
    id               uuid PRIMARY KEY,
    exam_id          uuid NOT NULL REFERENCES exams(id),
    key              text NOT NULL,
    type             text NOT NULL,
    stem             text NOT NULL,
    explanation      text,
    difficulty       text,
    source_kind      text NOT NULL,
    source_name      text NOT NULL,
    origin           jsonb,
    stem_fingerprint text NOT NULL,
    version          integer NOT NULL DEFAULT 1,
    ord              bigint GENERATED ALWAYS AS IDENTITY,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    deleted_at       timestamptz,
    CONSTRAINT question_key_pattern  CHECK (key ~ '^[a-z0-9][a-z0-9._-]{0,99}$'),
    CONSTRAINT question_type         CHECK (type IN ('single', 'multi')),
    CONSTRAINT question_stem_len     CHECK (char_length(stem) BETWEEN 1 AND 10000),
    CONSTRAINT question_expl_len     CHECK (explanation IS NULL OR char_length(explanation) BETWEEN 1 AND 20000),
    CONSTRAINT question_difficulty   CHECK (difficulty IS NULL OR difficulty IN ('easy', 'medium', 'hard')),
    CONSTRAINT question_source_kind  CHECK (source_kind IN ('purchased', 'generated', 'own', 'other')),
    CONSTRAINT question_source_name  CHECK (char_length(source_name) BETWEEN 1 AND 120),
    CONSTRAINT question_origin_obj   CHECK (origin IS NULL OR jsonb_typeof(origin) = 'object'),
    CONSTRAINT question_version_pos  CHECK (version >= 1)
);

CREATE UNIQUE INDEX questions_exam_key_alive_ux ON questions (exam_id, key) WHERE deleted_at IS NULL;
CREATE INDEX questions_exam_fingerprint_ix ON questions (exam_id, stem_fingerprint);

CREATE TRIGGER questions_no_delete BEFORE DELETE ON questions
    FOR EACH ROW EXECUTE FUNCTION forbid_hard_delete();

-- Options are replaced on update by marking the old set deleted and inserting
-- the new one, so only the alive set is unique by position.
CREATE TABLE question_options (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    question_id uuid NOT NULL REFERENCES questions(id),
    position    smallint NOT NULL,
    text        text,
    image       text,
    alt         text,
    correct     boolean NOT NULL,
    rationale   text,
    created_at  timestamptz NOT NULL DEFAULT now(),
    deleted_at  timestamptz,
    CONSTRAINT option_position CHECK (position BETWEEN 0 AND 5),
    CONSTRAINT option_content  CHECK (text IS NOT NULL OR image IS NOT NULL),
    CONSTRAINT option_alt      CHECK (image IS NULL OR alt IS NOT NULL),
    CONSTRAINT option_image    CHECK (image IS NULL OR image ~ '^media:[0-9a-f]{64}$')
);

CREATE UNIQUE INDEX question_options_position_alive_ux
    ON question_options (question_id, position) WHERE deleted_at IS NULL;

CREATE TRIGGER question_options_no_delete BEFORE DELETE ON question_options
    FOR EACH ROW EXECUTE FUNCTION forbid_hard_delete();

CREATE TABLE question_categories (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    question_id uuid NOT NULL REFERENCES questions(id),
    category_id uuid NOT NULL REFERENCES exam_categories(id),
    created_at  timestamptz NOT NULL DEFAULT now(),
    deleted_at  timestamptz
);

CREATE UNIQUE INDEX question_categories_alive_ux
    ON question_categories (question_id, category_id) WHERE deleted_at IS NULL;
CREATE INDEX question_categories_category_alive_ix
    ON question_categories (category_id) WHERE deleted_at IS NULL;

CREATE TRIGGER question_categories_no_delete BEFORE DELETE ON question_categories
    FOR EACH ROW EXECUTE FUNCTION forbid_hard_delete();
