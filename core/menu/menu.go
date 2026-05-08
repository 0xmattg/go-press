package menu

import (
	"context"
	"runtime"
	"sync"

	"go-press/core/hook"
	"go-press/pkg/dbprefix"

	"gorm.io/gorm"
)

// Menu represents a navigation menu.
type Menu struct {
	ID       uint   `gorm:"primaryKey" json:"id"`
	Name     string `gorm:"size:100;not null" json:"name"`
	Location string `gorm:"size:50" json:"location"`
	Items    []Item `gorm:"foreignKey:MenuID" json:"items"`
}

func (Menu) TableName() string { return dbprefix.Table("menus") }

// Item represents a single menu item.
type Item struct {
	ID        uint   `gorm:"primaryKey" json:"id"`
	MenuID    uint   `gorm:"not null" json:"menu_id"`
	ParentID  *uint  `json:"parent_id"`
	Title     string `gorm:"size:200;not null" json:"title"`
	URL       string `gorm:"size:500" json:"url"`
	Target    string `gorm:"size:20;default:_self" json:"target"`
	ContentID *uint  `json:"content_id"`
	SortOrder int    `gorm:"default:0" json:"sort_order"`
	Children  []Item `gorm:"-" json:"children"`
}

func (Item) TableName() string { return dbprefix.Table("menu_items") }

// LocationDef defines a registerable menu location (e.g. "header", "footer").
type LocationDef struct {
	Name  string
	Label string
}

// Store manages menus in memory with DB persistence.
type Store struct {
	mu        sync.RWMutex
	db        *gorm.DB
	menus     map[string]*Menu // keyed by location
	menusById map[uint]*Menu   // keyed by menu ID
	locations map[string]LocationDef
	hooks     *hook.Bus
}

// NewStore creates a new menu Store.
func NewStore(db *gorm.DB, hooks ...*hook.Bus) *Store {
	var hookBus *hook.Bus
	if len(hooks) > 0 {
		hookBus = hooks[0]
	}
	s := &Store{
		db:        db,
		menus:     make(map[string]*Menu),
		menusById: make(map[uint]*Menu),
		locations: make(map[string]LocationDef),
		hooks:     hookBus,
	}
	s.LoadAll()
	return s
}

// SetHookBus attaches the core hook bus after store creation.
func (s *Store) SetHookBus(hooks *hook.Bus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hooks = hooks
}

// --- Per-request language tracking (goroutine-local via sync.Map) ---

var requestLang sync.Map // goroutineID → string

// SetRequestLang sets the language for the current request's goroutine.
// Must be paired with ClearRequestLang in a defer.
func SetRequestLang(lang string) {
	requestLang.Store(goroutineID(), lang)
}

// ClearRequestLang removes the per-goroutine language hint.
func ClearRequestLang() {
	requestLang.Delete(goroutineID())
}

// GetRequestLang returns the language set for the current goroutine, or "".
func GetRequestLang() string {
	if v, ok := requestLang.Load(goroutineID()); ok {
		return v.(string)
	}
	return ""
}

func goroutineID() uint64 {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	// "goroutine 42 [running]:\n..."
	var id uint64
	for i := len("goroutine "); i < n && buf[i] != ' '; i++ {
		id = id*10 + uint64(buf[i]-'0')
	}
	return id
}

// RegisterLocation registers a theme menu location.
func (s *Store) RegisterLocation(name, label string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.locations[name] = LocationDef{Name: name, Label: label}
}

// GetLocations returns all registered menu locations.
func (s *Store) GetLocations() []LocationDef {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]LocationDef, 0, len(s.locations))
	for _, loc := range s.locations {
		result = append(result, loc)
	}
	return result
}

// LoadAll loads all menus and items from DB into memory.
// For each location, only the menu with the smallest ID is kept in the
// location map. Plugins may resolve a different menu at runtime through
// hook.MenuLocationResolve.
func (s *Store) LoadAll() {
	var menus []Menu
	s.db.Preload("Items", func(db *gorm.DB) *gorm.DB {
		return db.Order("sort_order ASC")
	}).Order("id ASC").Find(&menus)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.menus = make(map[string]*Menu)
	s.menusById = make(map[uint]*Menu)
	for i := range menus {
		m := &menus[i]
		m.Items = buildTree(m.Items, nil)
		s.menusById[m.ID] = m
		if m.Location != "" {
			// Keep only the first (lowest ID) menu per location
			if _, exists := s.menus[m.Location]; !exists {
				s.menus[m.Location] = m
			}
		}
	}
}

// GetByLocation returns the menu assigned to the given location.
// Plugins can filter the resolved menu via hook.MenuLocationResolve.
func (s *Store) GetByLocation(location string) *Menu {
	s.mu.RLock()
	m := s.menus[location]
	hooks := s.hooks
	s.mu.RUnlock()

	if hooks != nil && m != nil {
		if resolved, ok := hooks.ApplyFilter(hook.MenuLocationResolve, m, location).(*Menu); ok {
			return resolved
		}
	}
	return m
}

// GetByIDCached returns a menu by ID from the in-memory cache.
func (s *Store) GetByIDCached(id uint) *Menu {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.menusById[id]
}

// GetAll returns all menus from the database (with items, no tree build).
func (s *Store) GetAll() ([]Menu, error) {
	var menus []Menu
	err := s.db.Preload("Items", func(db *gorm.DB) *gorm.DB {
		return db.Order("sort_order ASC")
	}).Order("id ASC").Find(&menus).Error
	return menus, err
}

// GetByID returns a single menu by ID with its items built into a tree.
func (s *Store) GetByID(id uint) (*Menu, error) {
	var m Menu
	if err := s.db.Preload("Items", func(db *gorm.DB) *gorm.DB {
		return db.Order("sort_order ASC")
	}).First(&m, id).Error; err != nil {
		return nil, err
	}
	m.Items = buildTree(m.Items, nil)
	return &m, nil
}

// Create creates a new menu in the database and reloads the memory cache.
func (s *Store) Create(m *Menu) error {
	if err := s.db.Create(m).Error; err != nil {
		return err
	}
	s.LoadAll()
	return nil
}

// Update updates a menu's name and location.
func (s *Store) Update(m *Menu) error {
	if err := s.db.Save(m).Error; err != nil {
		return err
	}
	s.LoadAll()
	return nil
}

// Delete deletes a menu and all its items, then reloads the memory cache.
// hook.MenuDeleted is fired after deletion to allow plugin cleanup.
func (s *Store) Delete(id uint) error {
	if err := s.db.Where("menu_id = ?", id).Delete(&Item{}).Error; err != nil {
		return err
	}
	if err := s.db.Delete(&Menu{}, id).Error; err != nil {
		return err
	}

	s.mu.RLock()
	hooks := s.hooks
	s.mu.RUnlock()
	if hooks != nil {
		hooks.DoAction(context.Background(), hook.MenuDeleted, id)
	}

	s.LoadAll()
	return nil
}

// SaveItems replaces all items for a menu atomically.
// Items may have nested Children; parent IDs are resolved during insertion.
func (s *Store) SaveItems(menuID uint, items []Item) error {
	err := s.db.Transaction(func(tx *gorm.DB) error {
		// Delete existing items
		if err := tx.Where("menu_id = ?", menuID).Delete(&Item{}).Error; err != nil {
			return err
		}
		// Recursively insert items
		return insertItems(tx, menuID, nil, items)
	})
	if err != nil {
		return err
	}
	s.LoadAll()
	return nil
}

// insertItems recursively inserts menu items with correct parent IDs.
func insertItems(tx *gorm.DB, menuID uint, parentID *uint, items []Item) error {
	for i := range items {
		children := items[i].Children
		items[i].ID = 0
		items[i].MenuID = menuID
		items[i].ParentID = parentID
		items[i].Children = nil // prevent GORM from cascading
		if err := tx.Create(&items[i]).Error; err != nil {
			return err
		}
		if len(children) > 0 {
			newParentID := items[i].ID
			if err := insertItems(tx, menuID, &newParentID, children); err != nil {
				return err
			}
		}
	}
	return nil
}

// Reload reloads all menus from DB to memory.
func (s *Store) Reload() {
	s.LoadAll()
}

// buildTree converts a flat item list into a nested tree.
func buildTree(items []Item, parentID *uint) []Item {
	var tree []Item
	for _, item := range items {
		if ptrEqual(item.ParentID, parentID) {
			item.Children = buildTree(items, &item.ID)
			tree = append(tree, item)
		}
	}
	return tree
}

func ptrEqual(a, b *uint) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}
