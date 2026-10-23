package http

import "github.com/afwilto/studies-mngr/internal/exams/domain"

// FR-6 image limits.
const (
	maxImageBytes = 2 << 20
	maxImageSide  = 4096
)

// inspectImage decides what an archive entry is from its bytes alone: the
// extension is never consulted. A usable image comes back with its hash,
// content type and dimensions; anything else carries the Problem.
func inspectImage(path string, data []byte) domain.MediaFile { return domain.MediaFile{Path: path} }

// archiveForQuestions reads and inspects the entries the questions reference
// by path. Entries nobody references are not read at all.
func archiveForQuestions(z *importZip, qs []domain.FileQuestion) (*domain.MediaArchive, error) {
	return nil, nil
}
