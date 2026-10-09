DROP TRIGGER IF EXISTS categories_guard_subjects ON categories;
DROP FUNCTION IF EXISTS guard_category_subjects();
DROP TABLE IF EXISTS subjects;
DROP FUNCTION IF EXISTS guard_subject_category();
