-- Spec 16 FR-2: subjects belong to any alive category. Deletion is logical;
-- uniqueness applies among the alive subjects of a category.
CREATE TABLE subjects (
    id          uuid PRIMARY KEY,
    category_id uuid NOT NULL REFERENCES categories(id),
    name        text NOT NULL,
    description text,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    deleted_at  timestamptz,
    CONSTRAINT sub_name_len CHECK (char_length(name) BETWEEN 1 AND 120),
    CONSTRAINT sub_desc_len CHECK (description IS NULL OR char_length(description) <= 2000)
);

CREATE UNIQUE INDEX subjects_cat_name_alive_ux
    ON subjects (category_id, lower(name)) WHERE deleted_at IS NULL;

CREATE INDEX subjects_category_alive_ix ON subjects (category_id) WHERE deleted_at IS NULL;

CREATE TRIGGER subjects_no_delete BEFORE DELETE ON subjects
    FOR EACH ROW EXECUTE FUNCTION forbid_hard_delete();

-- Slice 04, S04-12: the category/subject relation is enforced by the database.
-- Both guards take the advisory lock the category-tree guard uses, so a subject
-- insert and a category delete serialize and cannot both succeed.
CREATE FUNCTION guard_subject_category() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.deleted_at IS NOT NULL THEN
        RETURN NEW;
    END IF;
    IF TG_OP = 'INSERT'
       OR NEW.category_id IS DISTINCT FROM OLD.category_id
       OR OLD.deleted_at IS NOT NULL THEN -- placement change or manual restore
        PERFORM pg_advisory_xact_lock(hashtext('studies.categories.tree'));
        IF NOT EXISTS (SELECT 1 FROM categories WHERE id = NEW.category_id AND deleted_at IS NULL) THEN
            RAISE EXCEPTION 'category % does not exist', NEW.category_id
                USING ERRCODE = 'check_violation', CONSTRAINT = 'subject_category_not_found';
        END IF;
    END IF;
    RETURN NEW;
END $$;

CREATE TRIGGER subjects_guard_category BEFORE INSERT OR UPDATE ON subjects
    FOR EACH ROW EXECUTE FUNCTION guard_subject_category();

CREATE FUNCTION guard_category_subjects() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.deleted_at IS NULL AND NEW.deleted_at IS NOT NULL THEN
        PERFORM pg_advisory_xact_lock(hashtext('studies.categories.tree'));
        IF EXISTS (SELECT 1 FROM subjects WHERE category_id = NEW.id AND deleted_at IS NULL) THEN
            RAISE EXCEPTION 'category % still has alive subjects', NEW.id
                USING ERRCODE = 'check_violation', CONSTRAINT = 'category_in_use';
        END IF;
    END IF;
    RETURN NEW;
END $$;

CREATE TRIGGER categories_guard_subjects BEFORE UPDATE OF deleted_at ON categories
    FOR EACH ROW EXECUTE FUNCTION guard_category_subjects();
