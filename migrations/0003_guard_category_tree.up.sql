-- Spec 16 slice 02, S02-8: the category tree's integrity is enforced by the
-- database, so no request ordering, concurrency or hand-written SQL can create
-- a cycle, exceed 5 levels, hang a child off a deleted parent, or delete a
-- parent that still has alive children. The use cases check the same rules
-- first for friendly messages; this trigger is the guarantee.
--
-- Errors carry a CONSTRAINT name the repository maps to domain errors:
--   category_parent_not_found, category_cycle, category_max_depth, category_in_use
CREATE FUNCTION guard_category_tree() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    parent_depth   int;
    is_cycle       bool;
    reaches_root   bool;
    subtree_height int;
BEGIN
    -- Serialize structural changes: concurrent moves/deletes see each other.
    PERFORM pg_advisory_xact_lock(hashtext('studies.categories.tree'));

    IF TG_OP = 'UPDATE' AND OLD.deleted_at IS NULL AND NEW.deleted_at IS NOT NULL THEN
        IF EXISTS (SELECT 1 FROM categories WHERE parent_id = NEW.id AND deleted_at IS NULL) THEN
            RAISE EXCEPTION 'category % still has alive subcategories', NEW.id
                USING ERRCODE = 'check_violation', CONSTRAINT = 'category_in_use';
        END IF;
    END IF;

    IF NEW.deleted_at IS NOT NULL THEN
        RETURN NEW; -- a deleted row is not placed in the tree
    END IF;

    IF TG_OP = 'INSERT'
       OR NEW.parent_id IS DISTINCT FROM OLD.parent_id
       OR OLD.deleted_at IS NOT NULL THEN -- placement change or manual restore

        IF NEW.parent_id IS NULL THEN
            parent_depth := 0;
        ELSE
            WITH RECURSIVE up AS (
                SELECT id, parent_id, 1 AS lvl
                FROM categories WHERE id = NEW.parent_id AND deleted_at IS NULL
                UNION ALL
                SELECT c.id, c.parent_id, up.lvl + 1
                FROM categories c JOIN up ON c.id = up.parent_id
                WHERE c.deleted_at IS NULL AND up.lvl < 10
            )
            SELECT count(*), coalesce(bool_or(id = NEW.id), false), coalesce(bool_or(parent_id IS NULL), false)
            INTO parent_depth, is_cycle, reaches_root
            FROM up;

            IF is_cycle THEN
                RAISE EXCEPTION 'category % cannot be placed under itself or a descendant', NEW.id
                    USING ERRCODE = 'check_violation', CONSTRAINT = 'category_cycle';
            END IF;
            IF parent_depth = 0 OR NOT reaches_root THEN
                RAISE EXCEPTION 'parent category % does not exist', NEW.parent_id
                    USING ERRCODE = 'check_violation', CONSTRAINT = 'category_parent_not_found';
            END IF;
        END IF;

        WITH RECURSIVE down AS (
            SELECT id, 1 AS lvl FROM categories WHERE id = NEW.id
            UNION ALL
            SELECT c.id, down.lvl + 1
            FROM categories c JOIN down ON c.parent_id = down.id
            WHERE c.deleted_at IS NULL AND down.lvl < 10
        )
        SELECT max(lvl) INTO subtree_height FROM down;

        IF parent_depth + coalesce(subtree_height, 1) > 5 THEN
            RAISE EXCEPTION 'category tree is limited to 5 levels'
                USING ERRCODE = 'check_violation', CONSTRAINT = 'category_max_depth';
        END IF;
    END IF;

    RETURN NEW;
END
$$;

CREATE TRIGGER categories_guard_tree
    BEFORE INSERT OR UPDATE OF parent_id, deleted_at ON categories
    FOR EACH ROW EXECUTE FUNCTION guard_category_tree();
