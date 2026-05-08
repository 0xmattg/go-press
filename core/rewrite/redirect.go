package rewrite

import (
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"go-press/pkg/dbprefix"
	"go-press/pkg/logger"
)

// Redirect represents a URL redirect stored in the database.
type Redirect struct {
	ID         uint   `gorm:"primaryKey"`
	SourcePath string `gorm:"size:500;uniqueIndex;not null"`
	TargetPath string `gorm:"size:500;not null"`
	StatusCode int    `gorm:"default:301"`
	HitCount   int64  `gorm:"default:0"`
}

func (Redirect) TableName() string { return dbprefix.Table("redirects") }

// RedirectManager handles 301/302 redirects with in-memory caching.
type RedirectManager struct {
	mu    sync.RWMutex
	db    *gorm.DB
	cache map[string]*Redirect // source_path → redirect
}

// NewRedirectManager creates and loads redirects into memory.
func NewRedirectManager(db *gorm.DB) *RedirectManager {
	rm := &RedirectManager{
		db:    db,
		cache: make(map[string]*Redirect),
	}
	rm.LoadAll()
	return rm
}

// LoadAll reads all redirects from DB into memory.
func (rm *RedirectManager) LoadAll() {
	var redirects []Redirect
	rm.db.Find(&redirects)

	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.cache = make(map[string]*Redirect, len(redirects))
	for i := range redirects {
		rm.cache[redirects[i].SourcePath] = &redirects[i]
	}
	logger.Info("Redirects loaded", "count", len(redirects))
}

// Lookup checks if a path has a redirect rule.
func (rm *RedirectManager) Lookup(path string) *Redirect {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	return rm.cache[path]
}

// Add creates a new redirect rule.
func (rm *RedirectManager) Add(source, target string, statusCode int) error {
	if statusCode == 0 {
		statusCode = 301
	}
	r := Redirect{
		SourcePath: source,
		TargetPath: target,
		StatusCode: statusCode,
	}
	if err := rm.db.Create(&r).Error; err != nil {
		return err
	}
	rm.mu.Lock()
	rm.cache[source] = &r
	rm.mu.Unlock()
	return nil
}

// Remove deletes a redirect rule.
func (rm *RedirectManager) Remove(source string) error {
	if err := rm.db.Where("source_path = ?", source).Delete(&Redirect{}).Error; err != nil {
		return err
	}
	rm.mu.Lock()
	delete(rm.cache, source)
	rm.mu.Unlock()
	return nil
}

// IncrementHit asynchronously increments the hit counter (fire-and-forget).
func (rm *RedirectManager) IncrementHit(source string) {
	go func() {
		rm.db.Model(&Redirect{}).
			Where("source_path = ?", source).
			UpdateColumn("hit_count", gorm.Expr("hit_count + 1"))
	}()
}

// Middleware returns a Gin middleware that handles redirects.
func (rm *RedirectManager) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		r := rm.Lookup(c.Request.URL.Path)
		if r != nil {
			rm.IncrementHit(r.SourcePath)
			c.Redirect(r.StatusCode, r.TargetPath)
			c.Abort()
			return
		}
		c.Next()
	}
}

// All returns all redirect rules (for admin display).
func (rm *RedirectManager) All() []Redirect {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	result := make([]Redirect, 0, len(rm.cache))
	for _, r := range rm.cache {
		result = append(result, *r)
	}
	return result
}

// Migrate ensures the redirects table exists.
func (rm *RedirectManager) Migrate() error {
	return rm.db.AutoMigrate(&Redirect{})
}

// Status codes for convenience.
const (
	StatusMovedPermanently = http.StatusMovedPermanently // 301
	StatusFound            = http.StatusFound            // 302
)
