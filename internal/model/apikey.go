package model

const (
	APIKeyModelAccessTypeGroup      = "group"
	APIKeyModelAccessTypeAllChannel = "all_channel"
)

type APIKey struct {
	ID              int     `json:"id" gorm:"primaryKey"`
	Name            string  `json:"name" gorm:"not null"`
	APIKey          string  `json:"api_key" gorm:"not null"`
	Enabled         bool    `json:"enabled" gorm:"default:true"`
	ExpireAt        int64   `json:"expire_at,omitempty"`
	MaxCost         float64 `json:"max_cost,omitempty"`
	SupportedModels string  `json:"supported_models,omitempty"`
	ModelAccessType string  `json:"model_access_type,omitempty" gorm:"default:'group'"`
}

func NormalizeAPIKeyModelAccessType(accessType string) string {
	switch accessType {
	case APIKeyModelAccessTypeGroup, APIKeyModelAccessTypeAllChannel:
		return accessType
	default:
		return APIKeyModelAccessTypeGroup
	}
}
