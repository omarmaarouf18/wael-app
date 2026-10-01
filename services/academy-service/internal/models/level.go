package models

// StudyType constants representing fixed academic categories.
const (
	StudyTypeBachelor   = "bachelor"
	StudyTypeDiploma    = "diploma"
	StudyTypeVocational = "vocational"
)

// Level represents an academic year or diploma/programme tier.
type Level struct {
	Key       string `bson:"key" json:"key"`
	StudyType string `bson:"study_type" json:"study_type"` // "bachelor", "diploma", "vocational"
	TitleAr   string `bson:"title_ar" json:"title_ar"`
	TitleEn   string `bson:"title_en" json:"title_en"`
	Position  int    `bson:"position" json:"position"`
}

// LocalizedText provides bilingual titles/descriptions with Arabic required and English optional.
type LocalizedText struct {
	Ar string `json:"ar"`
	En string `json:"en"`
}

// LevelDTO is the student-facing DTO for an academic level.
type LevelDTO struct {
	Key       string        `json:"key"`
	StudyType string        `json:"study_type"`
	Title     LocalizedText `json:"title"`
	Position  int           `json:"position"`
}

// StudyTypeDTO organizes levels under their study type in the catalog tree.
type StudyTypeDTO struct {
	Key    string        `json:"key"`
	Title  LocalizedText `json:"title"`
	Levels []LevelDTO    `json:"levels"`
}

// LevelsResponseDTO is the response body for GET /academy/levels.
type LevelsResponseDTO struct {
	Levels     []LevelDTO     `json:"levels"`
	StudyTypes []StudyTypeDTO `json:"study_types,omitempty"`
}

// ToDTO converts a Level model to its student-facing DTO.
func (l *Level) ToDTO() LevelDTO {
	return LevelDTO{
		Key:       l.Key,
		StudyType: l.StudyType,
		Title: LocalizedText{
			Ar: l.TitleAr,
			En: l.TitleEn,
		},
		Position: l.Position,
	}
}

// SeededLevels contains the fixed initial levels per SPEC Section 1 Decision 2 amendment and D12:
// Bachelor years 1-4 and the single vocational training level. Diplomas are admin-created later.
var SeededLevels = []Level{
	{
		Key:       "bachelor-y1",
		StudyType: StudyTypeBachelor,
		TitleAr:   "الفرقة الأولى",
		TitleEn:   "Year 1",
		Position:  1,
	},
	{
		Key:       "bachelor-y2",
		StudyType: StudyTypeBachelor,
		TitleAr:   "الفرقة الثانية",
		TitleEn:   "Year 2",
		Position:  2,
	},
	{
		Key:       "bachelor-y3",
		StudyType: StudyTypeBachelor,
		TitleAr:   "الفرقة الثالثة",
		TitleEn:   "Year 3",
		Position:  3,
	},
	{
		Key:       "bachelor-y4",
		StudyType: StudyTypeBachelor,
		TitleAr:   "الفرقة الرابعة",
		TitleEn:   "Year 4",
		Position:  4,
	},
	{
		Key:       "vocational",
		StudyType: StudyTypeVocational,
		TitleAr:   "التدريب المهني والعملي",
		TitleEn:   "Vocational Training",
		Position:  5,
	},
}
