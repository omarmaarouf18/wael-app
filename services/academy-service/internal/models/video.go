package models

import (
	"strings"
	"time"
)

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

// VideoMetadataDTO is the student-facing video representation in subject detail.
// Each video indicates whether the student may play it via Playable.
// The YouTube ID is NEVER included in metadata or subject detail;
// it is obtained per-play via POST /academy/videos/{id}/play.
type VideoMetadataDTO struct {
	ID          string        `json:"id"`
	Position    int           `json:"position"`
	Title       LocalizedText `json:"title"`
	Description LocalizedText `json:"description"`
	Playable    bool          `json:"playable"`
}

// ToDTO converts a Video to student-safe metadata with playback eligibility.
// playable = owned now AND video published AND youtube_video_id non-empty.
func (v *Video) ToDTO(owned bool) VideoMetadataDTO {
	isPlayable := owned && v.Published && strings.TrimSpace(v.YouTubeVideoID) != ""
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
		Playable: isPlayable,
	}
}

// VideoPlayResponseDTO is the response body for POST /academy/videos/{id}/play.
type VideoPlayResponseDTO struct {
	VideoID        string `json:"video_id"`
	YouTubeVideoID string `json:"youtube_video_id"`
}

// VideoPlay represents an append-only log entry of a video playback event.
// Deliberately contains no IP address.
type VideoPlay struct {
	ID        string    `bson:"_id" json:"id"`
	UserID    string    `bson:"user_id" json:"user_id"`
	VideoID   string    `bson:"video_id" json:"video_id"`
	SubjectID string    `bson:"subject_id" json:"subject_id"`
	PlayedAt  time.Time `bson:"played_at" json:"played_at"`
}
