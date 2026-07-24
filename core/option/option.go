package option

import (
	"sync"

	"go-press/pkg/dbprefix"

	"gorm.io/gorm"
)

// Option represents one persisted key-value configuration row.
//
// Autoload options are read into memory during Store.LoadAll and are intended
// for settings needed on most requests, such as site metadata, active theme,
// theme settings, and plugin state.
type Option struct {
	ID       uint   `gorm:"primaryKey"`
	Name     string `gorm:"size:200;uniqueIndex;not null"`
	Value    string `gorm:"type:text"`
	Autoload bool   `gorm:"default:true"`
}

func (Option) TableName() string { return dbprefix.Table("options") }

// Store provides cached access to database-backed options.
//
// Reads hit the in-memory map first, then fall back to the database for
// non-autoload values. Writes update both storage layers so callers can read
// their changes immediately without another LoadAll.
type Store struct {
	mu   sync.RWMutex
	db   *gorm.DB
	data map[string]string
}

// NewStore creates a new option Store.
// Call LoadAll() after database tables are ready to populate the cache.
func NewStore(db *gorm.DB) *Store {
	return &Store{
		db:   db,
		data: make(map[string]string),
	}
}

// NewMemoryStore returns a Store backed only by the given in-memory values with
// no database. Seeded keys are served from memory; missing-key reads return ""
// and writes update memory only. Intended for tests and in-memory/CLI usage.
func NewMemoryStore(values map[string]string) *Store {
	data := make(map[string]string, len(values))
	for k, v := range values {
		data[k] = v
	}
	return &Store{data: data}
}

// LoadAll loads all autoload options from the database into memory.
//
// Existing cached keys are retained unless overwritten by database rows. This
// lets seed/import flows load new values without discarding programmatically
// inserted defaults.
func (s *Store) LoadAll() {
	if s.db == nil {
		return
	}
	var opts []Option
	s.db.Where("autoload = ?", true).Find(&opts)
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, o := range opts {
		s.data[o.Name] = o.Value
	}
}

// Get returns the value of an option. It first checks memory, then falls back to DB.
func (s *Store) Get(name string) string {
	s.mu.RLock()
	if v, ok := s.data[name]; ok {
		s.mu.RUnlock()
		return v
	}
	s.mu.RUnlock()

	// Fallback to DB for non-autoload options
	if s.db == nil {
		return ""
	}
	var opt Option
	if err := s.db.Where("name = ?", name).First(&opt).Error; err != nil {
		return ""
	}
	// Cache it
	s.mu.Lock()
	s.data[opt.Name] = opt.Value
	s.mu.Unlock()
	return opt.Value
}

// GetDefault returns the option value or a default if not found.
func (s *Store) GetDefault(name, defaultValue string) string {
	v := s.Get(name)
	if v == "" {
		return defaultValue
	}
	return v
}

// Set updates or creates an option both in memory and in DB.
func (s *Store) Set(name, value string) error {
	if s.db == nil {
		s.mu.Lock()
		s.data[name] = value
		s.mu.Unlock()
		return nil
	}
	var opt Option
	result := s.db.Where("name = ?", name).First(&opt)
	if result.Error != nil {
		opt = Option{Name: name, Value: value, Autoload: true}
		if err := s.db.Create(&opt).Error; err != nil {
			return err
		}
	} else {
		opt.Value = value
		if err := s.db.Save(&opt).Error; err != nil {
			return err
		}
	}
	s.mu.Lock()
	s.data[name] = value
	s.mu.Unlock()
	return nil
}

// Delete removes an option from both memory and DB.
func (s *Store) Delete(name string) error {
	s.mu.Lock()
	delete(s.data, name)
	s.mu.Unlock()
	if s.db == nil {
		return nil
	}
	return s.db.Where("name = ?", name).Delete(&Option{}).Error
}

// All returns a copy of all cached options.
//
// Template code receives this copy as Settings, so callers can read it freely
// without mutating the Store's internal map.
func (s *Store) All() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make(map[string]string, len(s.data))
	for k, v := range s.data {
		result[k] = v
	}
	return result
}
