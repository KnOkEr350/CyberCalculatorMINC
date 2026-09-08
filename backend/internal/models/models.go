// Package models описывает основные сущности предметной области.
package models

import "time"

type Role string

const (
	RoleAdmin Role = "admin"
	RoleUser  Role = "user"
)

// EntityType — тот самый выбор в админке "кто он": организация или вуз.
type EntityType string

const (
	EntityOrganization EntityType = "organization"    // ИТ-организация (обязанная сторона соглашения)
	EntityEduInst      EntityType = "edu_institution" // вуз/СПО — партнёр
)

type Audience string

const (
	AudienceVuz     Audience = "vuz"
	AudienceKolledj Audience = "kolledj"
	AudienceSchool  Audience = "school"
)

type PeriodType string

const (
	PeriodPlan PeriodType = "plan"
	PeriodFact PeriodType = "fact"
)

type User struct {
	ID         string     `json:"id"`
	Email      string     `json:"email"`
	FullName   string     `json:"full_name"`
	Role       Role       `json:"role"`
	EntityType EntityType `json:"entity_type,omitempty"`
	PartnerID  *string    `json:"partner_id,omitempty"`
	IsActive   bool       `json:"is_active"`
	CreatedAt  time.Time  `json:"created_at"`
}

type Partner struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	PartnerKind     string    `json:"partner_kind"` // vuz|kolledj|school
	AgreementDate   *string   `json:"agreement_date,omitempty"`
	AgreementNumber string    `json:"agreement_number,omitempty"`
	OtherAgreement  string    `json:"other_agreement,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
}

type ActivityCategory struct {
	Code          string   `json:"code"`
	Name          string   `json:"name"`
	Obligation    string   `json:"obligation"` // mandatory|variable
	AudienceScope []string `json:"audience_scope"`
}

type Entry struct {
	ID           string                 `json:"id"`
	CategoryCode string                 `json:"category_code"`
	PartnerID    *string                `json:"partner_id,omitempty"`
	PeriodType   PeriodType             `json:"period_type"`
	ReportYear   int                    `json:"report_year"`
	Audience     Audience               `json:"audience"`
	Payload      map[string]interface{} `json:"payload"`
	AmountRub    float64                `json:"amount_rub"`
	CreatedBy    string                 `json:"created_by"`
	UpdatedBy    *string                `json:"updated_by,omitempty"`
	CreatedAt    time.Time              `json:"created_at"`
	UpdatedAt    time.Time              `json:"updated_at"`
}

type Attachment struct {
	ID                 string    `json:"id"`
	EntryID            string    `json:"entry_id"`
	FileName           string    `json:"file_name"`
	StoragePath        string    `json:"-"`
	ContentType        string    `json:"content_type"`
	SizeBytes          int64     `json:"size_bytes"`
	UploadedBy         string    `json:"uploaded_by"`
	UploadedAt         time.Time `json:"uploaded_at"`
	RetentionExpiresAt time.Time `json:"retention_expires_at"`
}

type EntryComment struct {
	ID          string    `json:"id"`
	EntryID     string    `json:"entry_id"`
	UserID      string    `json:"user_id"`
	CommentText string    `json:"comment_text"`
	CreatedAt   time.Time `json:"created_at"`
}

type AuditLogItem struct {
	ID          string                 `json:"id"`
	EntityType  string                 `json:"entity_type"`
	EntityID    *string                `json:"entity_id,omitempty"`
	Action      string                 `json:"action"`
	UserID      *string                `json:"user_id,omitempty"`
	CommentText *string                `json:"comment_text,omitempty"`
	OldValue    map[string]interface{} `json:"old_value,omitempty"`
	NewValue    map[string]interface{} `json:"new_value,omitempty"`
	CreatedAt   time.Time              `json:"created_at"`
}
