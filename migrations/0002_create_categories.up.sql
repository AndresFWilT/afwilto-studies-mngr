-- Spec 16 FR-1: hierarchical categories as an adjacency list. Deletion is
-- logical; uniqueness applies among alive siblings only, and roots are
-- siblings of each other (COALESCE to the nil uuid, since NULLs are distinct).
CREATE TABLE categories (
    id         uuid PRIMARY KEY,
    parent_id  uuid REFERENCES categories(id),
    name       text NOT NULL,
    color      char(7) NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz,
    CONSTRAINT cat_name_len  CHECK (char_length(name) BETWEEN 1 AND 60),
    CONSTRAINT cat_color_fmt CHECK (color ~ '^#[0-9A-F]{6}$'),
    CONSTRAINT cat_not_self  CHECK (parent_id IS NULL OR parent_id <> id)
);

CREATE UNIQUE INDEX categories_sibling_name_alive_ux
    ON categories (COALESCE(parent_id, '00000000-0000-0000-0000-000000000000'::uuid), lower(name))
    WHERE deleted_at IS NULL;

CREATE INDEX categories_parent_alive_ix ON categories (parent_id) WHERE deleted_at IS NULL;

CREATE TRIGGER categories_no_delete BEFORE DELETE ON categories
    FOR EACH ROW EXECUTE FUNCTION forbid_hard_delete();
