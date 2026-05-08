package multilang

import (
	"time"

	"go-press/pkg/dbprefix"
)

const pluginSlug = "multilang"

// Translation links a content entry to a translation group and language.
// Every content row that participates in translation gets one Translation record.
// Rows sharing the same Trid belong to the same "translation set".
type Translation struct {
	ID              uint      `gorm:"primaryKey" json:"id"`
	Trid            uint      `gorm:"not null;index:idx_trid" json:"trid"`                  // Translation group ID
	ContentID       uint      `gorm:"not null;uniqueIndex" json:"content_id"`               // FK → contents.id
	LanguageCode    string    `gorm:"size:10;not null;index:idx_trid" json:"language_code"` // e.g. "en", "zh"
	SourceContentID *uint     `json:"source_content_id"`                                    // original content cloned from (nullable)
	CreatedAt       time.Time `json:"created_at"`
}

func (Translation) TableName() string {
	return dbprefix.PluginTable(pluginSlug, "translations")
}

// Language represents an enabled language for the site.
type Language struct {
	ID        uint   `gorm:"primaryKey" json:"id"`
	Code      string `gorm:"size:10;not null;uniqueIndex" json:"code"`
	Name      string `gorm:"size:100;not null" json:"name"`
	Flag      string `gorm:"size:20" json:"flag"` // emoji flag e.g. 🇨🇳
	IsDefault bool   `gorm:"default:false" json:"is_default"`
	SortOrder int    `gorm:"default:0" json:"sort_order"`
	Active    bool   `gorm:"default:true" json:"active"`
}

func (Language) TableName() string {
	return dbprefix.PluginTable(pluginSlug, "languages")
}

// StringTranslation stores UI string translations (like WPML string translation).
// Domain groups related strings (e.g. "theme", "plugin", "admin").
type StringTranslation struct {
	ID           uint   `gorm:"primaryKey" json:"id"`
	Domain       string `gorm:"size:100;not null;index:idx_domain_name" json:"domain"`
	Name         string `gorm:"size:500;not null;index:idx_domain_name" json:"name"`
	LanguageCode string `gorm:"size:10;not null" json:"language_code"`
	Value        string `gorm:"type:text;not null" json:"value"`
	Status       string `gorm:"size:20;default:translated" json:"status"` // translated, needs-update, not-translated
}

func (StringTranslation) TableName() string {
	return dbprefix.PluginTable(pluginSlug, "string_translations")
}

// MenuTranslation links menus across languages (like content Translation but for menus).
// Menus sharing the same Trid are translations of each other.
type MenuTranslation struct {
	ID           uint   `gorm:"primaryKey" json:"id"`
	Trid         uint   `gorm:"not null;index:idx_menu_trid" json:"trid"`
	MenuID       uint   `gorm:"not null;uniqueIndex" json:"menu_id"`
	LanguageCode string `gorm:"size:10;not null;index:idx_menu_trid" json:"language_code"`
}

func (MenuTranslation) TableName() string {
	return dbprefix.PluginTable(pluginSlug, "menu_translations")
}
