package models

import "time"

// Video represents an unlisted YouTube lesson video reference inside a subject.
type Video struct {
	ID             string    `bson:"_id" json:"id"`
	SubjectID      string    `bson:"subject_id" json:"subject_id"`
	Position       int       `bson:"position" json:"position"`
	TitleAr        string    `bson:"title_ar" json:"title_ar"`
	TitleEn        string    `bson:"title_en" json:"title_en"`
	DescriptionAr  string    `bson:"description_ar" json:"description_ar"`
	DescriptionEn  string    `bson:"description_en" json:"description_en"`
	YouTubeVideoID string    `bson:"youtube_video_id" json:"youtube_video_id"`
	Published      bool      `bson:"published" json:"published"`
	CreatedAt      time.Time `bson:"created_at" json:"created_at"`
	UpdatedAt      time.Time `bson:"updated_at" json:"updated_at"`
}

// VideoMetadataDTO is the student-facing video representation.
// CRITICAL: youtube_video_id is intentionally omitted in this phase.
// It is returned ONLY in Phase 3 when student entitlement ownership holds.
type VideoMetadataDTO struct {
	ID          string        `json:"id"`
	Position    int           `json:"position"`
	Title       LocalizedText `json:"title"`
	Description LocalizedText `json:"description"`
}

// ToDTO converts a Video to student-safe metadata (no youtube_video_id).
func (v *Video) ToDTO() VideoMetadataDTO {
	return VideoMetadataDTO{
		ID:       v.ID,
		Position: v.Position,
		Title: LocalizedText{
			Ar: v.TitleAr,
			En: v.TitleEn,
		},
		Description: LocalizedText{
			Ar: v.DescriptionAr,
			En: v.DescriptionEn,
		},
	}
}
