-- Spec 18 slice 05: stored question images, content-addressed. One row per
-- SHA-256; the bytes live in the media directory (MEDIA_DIR) under
-- ab/cd/<sha256>. Rows are never deleted (forbid_hard_delete, Spec 16 FR-0):
-- attempt snapshots keep resolving their images.
CREATE TABLE media (
    sha256       char(64) PRIMARY KEY,
    content_type text NOT NULL,
    bytes        integer NOT NULL,
    width        integer NOT NULL,
    height       integer NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT media_sha_hex      CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    CONSTRAINT media_content_type CHECK (content_type IN ('image/png', 'image/jpeg', 'image/webp')),
    CONSTRAINT media_bytes_pos    CHECK (bytes > 0),
    CONSTRAINT media_size_pos     CHECK (width > 0 AND height > 0)
);

CREATE TRIGGER media_no_delete BEFORE DELETE ON media
    FOR EACH ROW EXECUTE FUNCTION forbid_hard_delete();
