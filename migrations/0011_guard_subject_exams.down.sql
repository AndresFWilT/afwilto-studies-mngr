DROP TRIGGER IF EXISTS subjects_guard_exams ON subjects;
DROP FUNCTION IF EXISTS guard_subject_exams();
DROP TRIGGER IF EXISTS exams_guard_subject ON exams;
DROP FUNCTION IF EXISTS guard_exam_subject();
