package multilang

import (
	"fmt"

	"gorm.io/gorm"
)

// Repository provides CRUD operations for multilang plugin tables.
type Repository struct {
	db *gorm.DB
}

// NewRepository creates a new multilang Repository.
func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// AutoMigrate creates or updates the plugin tables.
func (r *Repository) AutoMigrate() error {
	return r.db.AutoMigrate(&Translation{}, &Language{}, &StringTranslation{}, &MenuTranslation{})
}

// ---- Language CRUD ----

// ListLanguages returns all languages ordered by sort_order.
func (r *Repository) ListLanguages() ([]Language, error) {
	var langs []Language
	err := r.db.Order("sort_order ASC, id ASC").Find(&langs).Error
	return langs, err
}

// ActiveLanguages returns only active languages.
func (r *Repository) ActiveLanguages() ([]Language, error) {
	var langs []Language
	err := r.db.Where("active = ?", true).Order("sort_order ASC, id ASC").Find(&langs).Error
	return langs, err
}

// DefaultLanguage returns the language marked as default.
func (r *Repository) DefaultLanguage() (*Language, error) {
	var lang Language
	err := r.db.Where("is_default = ?", true).First(&lang).Error
	if err != nil {
		return nil, err
	}
	return &lang, nil
}

// GetLanguageByCode finds a language by its code.
func (r *Repository) GetLanguageByCode(code string) (*Language, error) {
	var lang Language
	err := r.db.Where("code = ?", code).First(&lang).Error
	if err != nil {
		return nil, err
	}
	return &lang, nil
}

// CreateLanguage creates a new language entry.
func (r *Repository) CreateLanguage(lang *Language) error {
	// If setting as default, unset any existing default
	if lang.IsDefault {
		r.db.Model(&Language{}).Where("is_default = ?", true).Update("is_default", false)
	}
	return r.db.Create(lang).Error
}

// UpdateLanguage updates an existing language.
func (r *Repository) UpdateLanguage(lang *Language) error {
	if lang.IsDefault {
		r.db.Model(&Language{}).Where("is_default = ? AND id != ?", true, lang.ID).Update("is_default", false)
	}
	return r.db.Save(lang).Error
}

// DeleteLanguage deletes a language by ID.
func (r *Repository) DeleteLanguage(id uint) error {
	return r.db.Delete(&Language{}, id).Error
}

// ---- Translation CRUD ----

// GetTranslation finds the translation record for a content ID.
func (r *Repository) GetTranslation(contentID uint) (*Translation, error) {
	var t Translation
	err := r.db.Where("content_id = ?", contentID).First(&t).Error
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// GetTranslationsByTrid returns all translations in a translation group.
func (r *Repository) GetTranslationsByTrid(trid uint) ([]Translation, error) {
	var ts []Translation
	err := r.db.Where("trid = ?", trid).Find(&ts).Error
	return ts, err
}

// GetTranslatedContentID finds the content ID for a given trid + language.
func (r *Repository) GetTranslatedContentID(trid uint, langCode string) (uint, error) {
	var t Translation
	err := r.db.Where("trid = ? AND language_code = ?", trid, langCode).First(&t).Error
	if err != nil {
		return 0, err
	}
	return t.ContentID, nil
}

// FindTranslation finds a translation by content ID and language code for a given trid.
func (r *Repository) FindTranslation(trid uint, langCode string) (*Translation, error) {
	var t Translation
	err := r.db.Where("trid = ? AND language_code = ?", trid, langCode).First(&t).Error
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// NextTrid returns the next available translation group ID.
func (r *Repository) NextTrid() (uint, error) {
	var max *uint
	err := r.db.Model(&Translation{}).Select("MAX(trid)").Scan(&max).Error
	if err != nil {
		return 0, err
	}
	if max == nil {
		return 1, nil
	}
	return *max + 1, nil
}

// CreateTranslation creates a new translation link.
func (r *Repository) CreateTranslation(t *Translation) error {
	// Validate: no duplicate content_id→language in same trid
	var count int64
	r.db.Model(&Translation{}).Where("trid = ? AND language_code = ?", t.Trid, t.LanguageCode).Count(&count)
	if count > 0 {
		return fmt.Errorf("translation already exists for trid=%d lang=%s", t.Trid, t.LanguageCode)
	}
	return r.db.Create(t).Error
}

// DeleteTranslation removes a translation record by content ID.
func (r *Repository) DeleteTranslation(contentID uint) error {
	return r.db.Where("content_id = ?", contentID).Delete(&Translation{}).Error
}

// ---- String Translation CRUD ----

// GetStringTranslation finds a string translation by domain, name, and language.
func (r *Repository) GetStringTranslation(domain, name, langCode string) (string, error) {
	var st StringTranslation
	err := r.db.Where("domain = ? AND name = ? AND language_code = ?", domain, name, langCode).First(&st).Error
	if err != nil {
		return "", err
	}
	return st.Value, nil
}

// ListStringTranslations returns all string translations for a domain and language.
func (r *Repository) ListStringTranslations(domain, langCode string) ([]StringTranslation, error) {
	var sts []StringTranslation
	err := r.db.Where("domain = ? AND language_code = ?", domain, langCode).Find(&sts).Error
	return sts, err
}

// UpsertStringTranslation creates or updates a string translation.
func (r *Repository) UpsertStringTranslation(st *StringTranslation) error {
	var existing StringTranslation
	err := r.db.Where("domain = ? AND name = ? AND language_code = ?", st.Domain, st.Name, st.LanguageCode).First(&existing).Error
	if err == nil {
		// Update existing
		existing.Value = st.Value
		existing.Status = st.Status
		return r.db.Save(&existing).Error
	}
	return r.db.Create(st).Error
}

// DeleteStringTranslation removes a string translation.
func (r *Repository) DeleteStringTranslation(id uint) error {
	return r.db.Delete(&StringTranslation{}, id).Error
}

// ListStringTranslationsByLang returns all string translations for a language (all domains).
func (r *Repository) ListStringTranslationsByLang(langCode string) ([]StringTranslation, error) {
	var sts []StringTranslation
	err := r.db.Where("language_code = ?", langCode).Find(&sts).Error
	return sts, err
}

// DeleteStringTranslationByKey removes a string translation by domain, name, and language.
func (r *Repository) DeleteStringTranslationByKey(domain, name, langCode string) error {
	return r.db.Where("domain = ? AND name = ? AND language_code = ?", domain, name, langCode).Delete(&StringTranslation{}).Error
}

// ---- Menu Translation CRUD ----

// GetMenuTranslation returns the menu translation record for a menu ID.
func (r *Repository) GetMenuTranslation(menuID uint) (*MenuTranslation, error) {
	var mt MenuTranslation
	err := r.db.Where("menu_id = ?", menuID).First(&mt).Error
	if err != nil {
		return nil, err
	}
	return &mt, nil
}

// GetMenuTranslationsByTrid returns all menu translations in a translation group.
func (r *Repository) GetMenuTranslationsByTrid(trid uint) ([]MenuTranslation, error) {
	var mts []MenuTranslation
	err := r.db.Where("trid = ?", trid).Find(&mts).Error
	return mts, err
}

// GetTranslatedMenuID finds the menu ID for a given trid + language.
func (r *Repository) GetTranslatedMenuID(trid uint, langCode string) (uint, error) {
	var mt MenuTranslation
	err := r.db.Where("trid = ? AND language_code = ?", trid, langCode).First(&mt).Error
	if err != nil {
		return 0, err
	}
	return mt.MenuID, nil
}

// NextMenuTrid returns the next available menu translation group ID.
func (r *Repository) NextMenuTrid() (uint, error) {
	var max *uint
	err := r.db.Model(&MenuTranslation{}).Select("MAX(trid)").Scan(&max).Error
	if err != nil {
		return 0, err
	}
	if max == nil {
		return 1, nil
	}
	return *max + 1, nil
}

// LinkMenuTranslation creates or updates a menu translation link.
func (r *Repository) LinkMenuTranslation(menuID uint, langCode string, trid uint) error {
	var existing MenuTranslation
	err := r.db.Where("menu_id = ?", menuID).First(&existing).Error
	if err == nil {
		existing.LanguageCode = langCode
		existing.Trid = trid
		return r.db.Save(&existing).Error
	}
	return r.db.Create(&MenuTranslation{
		Trid:         trid,
		MenuID:       menuID,
		LanguageCode: langCode,
	}).Error
}

// UnlinkMenuTranslation removes a menu from its translation group.
func (r *Repository) UnlinkMenuTranslation(menuID uint) error {
	return r.db.Where("menu_id = ?", menuID).Delete(&MenuTranslation{}).Error
}

// ListAllMenuTranslations returns all menu translation records.
func (r *Repository) ListAllMenuTranslations() ([]MenuTranslation, error) {
	var mts []MenuTranslation
	err := r.db.Order("trid ASC, language_code ASC").Find(&mts).Error
	return mts, err
}
