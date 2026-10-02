package models

import (
	"strings"
	"time"
)

// Video represents an unlisted YouTube lesson video reference inside a subject.
// Deleted implements soft delete: deleted videos are hidden from students,
// from counts, and from /play. They are never hard-deleted.
type Video struct {
	ID              string    `bson:"_id" json:"id"`
	SubjectID       string    `bson:"subject_id" json:"subject_id"`
	Position        int       `bson:"position" json:"position"`
	TitleAr         string    `bson:"title_ar" json:"title_ar"`
	TitleEn         string    `bson:"title_en" json:"title_en"`
	DescriptionAr   string    `bson:"description_ar" json:"description_ar"`
	DescriptionEn   string    `bson:"description_en" json:"description_en"`
	YouTubeVideoID  string    `bson:"youtube_video_id" json:"youtube_video_id"`
	DurationSeconds int       `bson:"duration_seconds" json:"duration_seconds"`
	Published       bool      `bson:"published" json:"published"`
	Deleted         bool      `bson:"deleted" json:"-"`
	DeletedAt       time.Time `bson:"deleted_at,omitempty" json:"-"`
	CreatedAt       time.Time `bson:"created_at" json:"created_at"`
	UpdatedAt       time.Time `bson:"updated_at" json:"updated_at"`
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

// VideoAdminDTO is the admin view of a video. Unlike the student DTOs it
// includes the YouTube id.
type VideoAdminDTO struct {
	ID              string    `json:"id"`
	SubjectID       string    `json:"subject_id"`
	TitleAr         string    `json:"title_ar"`
	YouTubeVideoID  string    `json:"youtube_video_id"`
	Order           int       `json:"order"`
	DurationSeconds int       `json:"duration_seconds"`
	Published       bool      `json:"published"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// ToAdminDTO converts a Video to its admin representation.
func (v *Video) ToAdminDTO() VideoAdminDTO {
	return VideoAdminDTO{
		ID:              v.ID,
		SubjectID:       v.SubjectID,
		TitleAr:         v.TitleAr,
		YouTubeVideoID:  v.YouTubeVideoID,
		Order:           v.Position,
		DurationSeconds: v.DurationSeconds,
		Published:       v.Published,
		CreatedAt:       v.CreatedAt,
		UpdatedAt:       v.UpdatedAt,
	}
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
