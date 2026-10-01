package models

import "time"

// SubjectFile represents a PDF document (book or note) attached to a subject.
type SubjectFile struct {
	ID         string    `bson:"_id" json:"id"`
	SubjectID  string    `bson:"subject_id" json:"subject_id"`
	Kind       string    `bson:"kind" json:"kind"` // "book", "note"
	TitleAr    string    `bson:"title_ar" json:"title_ar"`
	TitleEn    string    `bson:"title_en" json:"title_en"`
	SizeBytes  int64     `bson:"size_bytes" json:"size_bytes"`
	StorageKey string    `bson:"storage_key" json:"storage_key"`
	CreatedAt  time.Time `bson:"created_at" json:"created_at"`
}

// FileMetadataDTO is the student-facing DTO for an attached PDF file.
// storage_key is never exposed to students.
type FileMetadataDTO struct {
	ID        string        `json:"id"`
	Kind      string        `json:"kind"`
	Title     LocalizedText `json:"title"`
	SizeBytes int64         `json:"size_bytes"`
}

// ToDTO converts a SubjectFile model to its student-facing DTO.
func (f *SubjectFile) ToDTO() FileMetadataDTO {
	return FileMetadataDTO{
		ID:   f.ID,
		Kind: f.Kind,
		Title: LocalizedText{
			Ar: f.TitleAr,
			En: f.TitleEn,
		},
		SizeBytes: f.SizeBytes,
	}
}
