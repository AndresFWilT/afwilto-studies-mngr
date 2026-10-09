DROP TRIGGER IF EXISTS note_kinds_guard_notes ON note_kinds;
DROP FUNCTION IF EXISTS guard_kind_notes();
DROP TRIGGER IF EXISTS subjects_guard_notes ON subjects;
DROP FUNCTION IF EXISTS guard_subject_notes();
DROP TABLE IF EXISTS notes;
DROP FUNCTION IF EXISTS guard_note_refs();
