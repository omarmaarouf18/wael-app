package models

import "time"

// Subject status constants.
const (
	StatusDraft     = "draft"
	StatusPublished = "published"
)

// Subject represents a course/subject inside an academic level.
type Subject struct {
	ID              string    `bson:"_id" json:"id"`
	LevelKey        string    `bson:"level_key" json:"level_key"`
	Term            string    `bson:"term" json:"term"` // "first", "second", ""
	TitleAr         string    `bson:"title_ar" json:"title_ar"`
	TitleEn         string    `bson:"title_en" json:"title_en"`
	DescriptionAr   string    `bson:"description_ar" json:"description_ar"`
	DescriptionEn   string    `bson:"description_en" json:"description_en"`
	Price           int       `bson:"price" json:"price"`
	Status          string    `bson:"status" json:"status"` // "draft", "published"
	Order           int       `bson:"order" json:"order"`
	AccessExpiresAt time.Time `bson:"access_expires_at" json:"access_expires_at"`
	CreatedAt       time.Time `bson:"created_at" json:"created_at"`
	UpdatedAt       time.Time `bson:"updated_at" json:"updated_at"`
}

// SubjectCountsDTO holds the count of content items inside a subject.
type SubjectCountsDTO struct {
	Videos int `json:"videos"`
	Books  int `json:"books"`
	Notes  int `json:"notes"`
}

// SubjectListItemDTO is the summary DTO returned in GET /academy/subjects.
type SubjectListItemDTO struct {
	ID              string           `json:"id"`
	LevelKey        string           `json:"level_key"`
	Term            string           `json:"term"`
	Title           LocalizedText    `json:"title"`
	Description     LocalizedText    `json:"description"`
	Owned           bool             `json:"owned"`
	Counts          SubjectCountsDTO `json:"counts"`
	Price           *int             `json:"price,omitempty"`
	Currency        string           `json:"currency,omitempty"`
	AccessExpiresAt time.Time        `json:"access_expires_at"`
}

// SubjectDetailDTO is the full DTO returned in GET /academy/subjects/{id}.
// Videos carry NO youtube_video_id in this phase.
type SubjectDetailDTO struct {
	ID              string             `json:"id"`
	LevelKey        string             `json:"level_key"`
	Term            string             `json:"term"`
	Title           LocalizedText      `json:"title"`
	Description     LocalizedText      `json:"description"`
	Owned           bool               `json:"owned"`
	Counts          SubjectCountsDTO   `json:"counts"`
	Videos          []VideoMetadataDTO `json:"videos"`
	Files           []FileMetadataDTO  `json:"files"`
	Price           *int               `json:"price,omitempty"`
	Currency        string             `json:"currency,omitempty"`
	AccessExpiresAt time.Time          `json:"access_expires_at"`
	Request         *SubjectRequestDTO `json:"request,omitempty"`
}

// SubjectListResponseDTO is the paginated response for GET /academy/subjects.
type SubjectListResponseDTO struct {
	Items []SubjectListItemDTO `json:"items"`
	Total int                  `json:"total"`
	Page  int                  `json:"page"`
	Limit int                  `json:"limit"`
}

// ToListItemDTO converts a Subject to its list representation.
func (s *Subject) ToListItemDTO(counts SubjectCountsDTO, owned bool, exposePrice bool) SubjectListItemDTO {
	dto := SubjectListItemDTO{
		ID:       s.ID,
		LevelKey: s.LevelKey,
		Term:     s.Term,
		Title: LocalizedText{
			Ar: s.TitleAr,
			En: s.TitleEn,
		},
		Description: LocalizedText{
			Ar: s.DescriptionAr,
			En: s.DescriptionEn,
		},
		Owned:           owned,
		Counts:          counts,
		AccessExpiresAt: s.AccessExpiresAt,
	}
	if exposePrice {
		p := s.Price
		dto.Price = &p
		dto.Currency = "EGP"
	}
	return dto
}

// ToDetailDTO converts a Subject to its detailed representation.
func (s *Subject) ToDetailDTO(counts SubjectCountsDTO, videos []VideoMetadataDTO, files []FileMetadataDTO, owned bool, exposePrice bool) SubjectDetailDTO {
	if videos == nil {
		videos = []VideoMetadataDTO{}
	}
	if files == nil {
		files = []FileMetadataDTO{}
	}
	dto := SubjectDetailDTO{
		ID:       s.ID,
		LevelKey: s.LevelKey,
		Term:     s.Term,
		Title: LocalizedText{
			Ar: s.TitleAr,
			En: s.TitleEn,
		},
		Description: LocalizedText{
			Ar: s.DescriptionAr,
			En: s.DescriptionEn,
		},
		Owned:           owned,
		Counts:          counts,
		Videos:          videos,
		Files:           files,
		AccessExpiresAt: s.AccessExpiresAt,
	}
	if exposePrice {
		p := s.Price
		dto.Price = &p
		dto.Currency = "EGP"
	}
	return dto
}
