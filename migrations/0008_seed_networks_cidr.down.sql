-- Spec 17 FR-10 down: logically delete (never SQL DELETE, Spec 16 FR-0) only
-- the rows migration 0008 created, identified by their fixed ids, and only
-- while nothing else alive depends on them. The owner's own rows are untouched.
UPDATE note_tags SET deleted_at = now()
 WHERE note_id = '00000000-0000-7000-8017-000000000004' AND deleted_at IS NULL;

UPDATE notes SET deleted_at = now(), updated_at = now()
 WHERE id = '00000000-0000-7000-8017-000000000004' AND deleted_at IS NULL;

UPDATE note_kinds SET deleted_at = now(), updated_at = now()
 WHERE id = '00000000-0000-7000-8017-000000000005' AND deleted_at IS NULL
   AND NOT EXISTS (SELECT 1 FROM notes WHERE kind_id = '00000000-0000-7000-8017-000000000005' AND deleted_at IS NULL);

UPDATE subjects SET deleted_at = now(), updated_at = now()
 WHERE id = '00000000-0000-7000-8017-000000000003' AND deleted_at IS NULL
   AND NOT EXISTS (SELECT 1 FROM notes WHERE subject_id = '00000000-0000-7000-8017-000000000003' AND deleted_at IS NULL);

UPDATE categories SET deleted_at = now(), updated_at = now()
 WHERE id = '00000000-0000-7000-8017-000000000002' AND deleted_at IS NULL
   AND NOT EXISTS (SELECT 1 FROM categories WHERE parent_id = '00000000-0000-7000-8017-000000000002' AND deleted_at IS NULL)
   AND NOT EXISTS (SELECT 1 FROM subjects WHERE category_id = '00000000-0000-7000-8017-000000000002' AND deleted_at IS NULL);

UPDATE categories SET deleted_at = now(), updated_at = now()
 WHERE id = '00000000-0000-7000-8017-000000000001' AND deleted_at IS NULL
   AND NOT EXISTS (SELECT 1 FROM categories WHERE parent_id = '00000000-0000-7000-8017-000000000001' AND deleted_at IS NULL)
   AND NOT EXISTS (SELECT 1 FROM subjects WHERE category_id = '00000000-0000-7000-8017-000000000001' AND deleted_at IS NULL);
