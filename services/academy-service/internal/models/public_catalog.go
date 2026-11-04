package models

import "time"

// AcademyInstructor is the platform's one teacher (SPEC Section 1 decision 1),
// served as every subject's instructor on the public catalog (W2a). Subjects
// carry no instructor field; the app shows the same name from
// frontend/lib/content/director_profile.dart. Keep the two equal.
var AcademyInstructor = LocalizedText{Ar: "وائل السعيد", En: "Wael El Saeed"}

// PublicSubjectDTO is one subject on the public catalog (ADR-0014 W2a,
// GET /academy/public/catalog and /academy/public/subjects/{id}). It is sales
// data only: never video ids, YouTube ids, file ids, storage keys, download
// URLs or anything else that opens content. The price is always present
// (ADR-0014 owner decision 5); app_settings.show_prices does not apply here.
type PublicSubjectDTO struct {
	ID              string           `json:"id"`
	LevelKey        string           `json:"level_key"`
	Term            string           `json:"term"`
	Title           LocalizedText    `json:"title"`
	Description     LocalizedText    `json:"description"`
	Price           int              `json:"price"`
	Currency        string           `json:"currency"`
	Instructor      LocalizedText    `json:"instructor"`
	Counts          SubjectCountsDTO `json:"counts"`
	AccessExpiresAt time.Time        `json:"access_expires_at"`
}

// PublicLevelDTO is a published level with its published subjects.
type PublicLevelDTO struct {
	Key       string             `json:"key"`
	StudyType string             `json:"study_type"`
	Title     LocalizedText      `json:"title"`
	Position  int                `json:"position"`
	Subjects  []PublicSubjectDTO `json:"subjects"`
}

// PublicStudyTypeDTO groups levels under their study type, in the fixed
// order of GET /academy/levels.
type PublicStudyTypeDTO struct {
	Key    string           `json:"key"`
	Title  LocalizedText    `json:"title"`
	Levels []PublicLevelDTO `json:"levels"`
}

// PublicCatalogDTO is the GET /academy/public/catalog response.
type PublicCatalogDTO struct {
	StudyTypes []PublicStudyTypeDTO `json:"study_types"`
}

// PublicSubjectDetailDTO is the GET /academy/public/subjects/{id} response:
// the catalog fields plus the subject's level.
type PublicSubjectDetailDTO struct {
	PublicSubjectDTO
	Level LevelDTO `json:"level"`
}

// ToPublicDTO converts a Subject to its public catalog representation.
func (s *Subject) ToPublicDTO(counts SubjectCountsDTO) PublicSubjectDTO {
	return PublicSubjectDTO{
		ID:              s.ID,
		LevelKey:        s.LevelKey,
		Term:            s.Term,
		Title:           LocalizedText{Ar: s.TitleAr, En: s.TitleEn},
		Description:     LocalizedText{Ar: s.DescriptionAr, En: s.DescriptionEn},
		Price:           s.Price,
		Currency:        "EGP",
		Instructor:      AcademyInstructor,
		Counts:          counts,
		AccessExpiresAt: s.AccessExpiresAt,
	}
}
