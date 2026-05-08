package core

import (
	"crypto/rand"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go-press/core/content"
	coreMedia "go-press/core/media"
	"go-press/core/option"
	"go-press/core/taxonomy"
	"go-press/pkg/dbprefix"
	"go-press/pkg/logger"

	"github.com/BurntSushi/toml"
)

// SeedData represents the declarative seed.toml structure.
type SeedData struct {
	// Admin is retained only so older seed files still parse. Demo imports must
	// never create or reset users; installer owns the initial administrator.
	Admin      SeedAdmin      `toml:"admin"`
	Settings   []SeedSetting  `toml:"settings"`
	Contents   []SeedContent  `toml:"contents"`
	Categories []SeedTaxonomy `toml:"categories"`
	Tags       []SeedTaxonomy `toml:"tags"`
}

type SeedAdmin struct {
	Username    string `toml:"username"`
	Email       string `toml:"email"`
	Password    string `toml:"password"`
	DisplayName string `toml:"display_name"`
	Role        string `toml:"role"`
}

type SeedSetting struct {
	Key   string `toml:"key"`
	Value string `toml:"value"`
}

type SeedContent struct {
	Type        string            `toml:"type"`
	Title       string            `toml:"title"`
	Slug        string            `toml:"slug"`
	Content     string            `toml:"content"`
	Description string            `toml:"description"`
	Excerpt     string            `toml:"excerpt"`
	ImageURL    string            `toml:"image_url"`
	SortOrder   int               `toml:"sort_order"`
	Category    string            `toml:"category"`
	Tags        []string          `toml:"tags"`
	Meta        map[string]string `toml:"meta"`
}

type SeedTaxonomy struct {
	Name string `toml:"name"`
	Slug string `toml:"slug"`
}

// coreOptionKeys lists option keys that must survive a demo-data import.
var coreOptionKeys = []string{
	"active_theme", "site_name", "site_description", "site_language",
	"admin_language", "admin_email", "powered_by_gopress",
}

// ForceSeedFromFile clears existing content, taxonomy, media, and option data,
// then re-seeds from the given TOML file. It intentionally preserves users;
// installer owns administrator creation and credentials.
func (e *Engine) ForceSeedFromFile(path string) error {
	logger.Info("Force-reseed requested — clearing existing data")

	// Preserve core options before clearing.
	preserved := make(map[string]string)
	for _, key := range coreOptionKeys {
		if v := e.Options.Get(key); v != "" {
			preserved[key] = v
		}
	}
	// Also preserve demo_imported_* and plugin_active_* flags.
	allOpts := e.Options.All()
	for k, v := range allOpts {
		if strings.HasPrefix(k, "demo_imported_") || strings.HasPrefix(k, "plugin_active_") {
			preserved[k] = v
		}
	}

	for _, tbl := range forceSeedClearTables() {
		if err := e.DB.Exec("DELETE FROM " + tbl).Error; err != nil {
			logger.Warn("Could not clear table", "table", tbl, "error", err)
		}
	}

	if err := e.seedFromFile(path, true); err != nil {
		return err
	}

	// Reload from DB so memory cache reflects the seed state.
	e.Options.LoadAll()

	// Restore preserved core options unconditionally.
	for k, v := range preserved {
		_ = e.Options.Set(k, v)
	}

	return nil
}

func forceSeedClearTables() []string {
	return []string{
		dbprefix.Table("term_relationships"),
		dbprefix.Table("content_meta"),
		dbprefix.Table("contents"),
		dbprefix.Table("taxonomies"),
		dbprefix.Table("terms"),
		dbprefix.Table("options"),
		dbprefix.Table("media"),
	}
}

// SeedFromFile reads a seed.toml file and populates the database.
func (e *Engine) SeedFromFile(path string) error {
	return e.seedFromFile(path, false)
}

func (e *Engine) seedFromFile(path string, force bool) error {
	// Check if file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		logger.Info("No seed file found, skipping", "path", path)
		return nil
	}

	if !force {
		// Check if already seeded
		var contentCount int64
		e.DB.Model(&content.Content{}).Count(&contentCount)
		if contentCount > 0 {
			logger.Info("Database already seeded, skipping")
			return nil
		}
	}

	logger.Info("Seeding from file", "path", path)

	var data SeedData
	if _, err := toml.DecodeFile(path, &data); err != nil {
		return fmt.Errorf("failed to parse seed file: %w", err)
	}

	// 1. Settings → options
	if len(data.Settings) > 0 {
		opts := make([]option.Option, len(data.Settings))
		for i, s := range data.Settings {
			opts[i] = option.Option{Name: s.Key, Value: s.Value, Autoload: true}
		}
		if err := e.DB.Create(&opts).Error; err != nil {
			return fmt.Errorf("failed to seed settings: %w", err)
		}
	}

	// 2. Categories
	catTaxMap := make(map[string]uint) // slug → taxonomy ID
	for _, cat := range data.Categories {
		term := taxonomy.Term{Name: cat.Name, Slug: cat.Slug}
		if err := e.DB.Create(&term).Error; err != nil {
			return fmt.Errorf("failed to create category term %q: %w", cat.Name, err)
		}
		tax := taxonomy.Taxonomy{TermID: term.ID, Taxonomy: "category"}
		if err := e.DB.Create(&tax).Error; err != nil {
			return fmt.Errorf("failed to create category taxonomy %q: %w", cat.Name, err)
		}
		catTaxMap[cat.Slug] = tax.ID
	}

	// 3. Tags
	tagTaxMap := make(map[string]uint) // slug → taxonomy ID
	for _, tag := range data.Tags {
		term := taxonomy.Term{Name: tag.Name, Slug: tag.Slug}
		if err := e.DB.Create(&term).Error; err != nil {
			return fmt.Errorf("failed to create tag term %q: %w", tag.Name, err)
		}
		tax := taxonomy.Taxonomy{TermID: term.ID, Taxonomy: "tag"}
		if err := e.DB.Create(&tax).Error; err != nil {
			return fmt.Errorf("failed to create tag taxonomy %q: %w", tag.Name, err)
		}
		tagTaxMap[tag.Slug] = tax.ID
	}

	// 4. Contents
	uploadDir := e.Config.CMS.UploadDir
	now := time.Now()
	for _, c := range data.Contents {
		// Use description or content field
		body := c.Content
		if body == "" {
			body = c.Description
		}

		// Download remote image to local uploads directory
		imageURL := c.ImageURL
		if strings.HasPrefix(imageURL, "http://") || strings.HasPrefix(imageURL, "https://") {
			if local, err := downloadSeedImage(imageURL, uploadDir, e.Media); err != nil {
				logger.Warn("Failed to download seed image, keeping remote URL",
					"url", imageURL, "error", err)
			} else {
				imageURL = local
			}
		}

		item := content.Content{
			Type:        c.Type,
			Status:      content.StatusPublished,
			Title:       c.Title,
			Slug:        c.Slug,
			Content:     body,
			Excerpt:     c.Excerpt,
			ImageURL:    imageURL,
			SortOrder:   c.SortOrder,
			PublishedAt: &now,
		}
		if err := e.DB.Create(&item).Error; err != nil {
			return fmt.Errorf("failed to create content %q: %w", c.Title, err)
		}

		// Save meta fields
		for k, v := range c.Meta {
			_ = e.Content.SaveMeta(item.ID, k, v)
		}

		// Link to category
		if c.Category != "" {
			if taxID, ok := catTaxMap[c.Category]; ok {
				_ = e.Taxonomy.SetContentTaxonomies(item.ID, []uint{taxID})
			}
		}

		// Link to tags
		if len(c.Tags) > 0 {
			var tagIDs []uint
			for _, slug := range c.Tags {
				if tid, ok := tagTaxMap[slug]; ok {
					tagIDs = append(tagIDs, tid)
				}
			}
			if len(tagIDs) > 0 {
				// Merge with existing (category may have been set)
				existing, _ := e.Taxonomy.GetContentTaxonomies(item.ID, "")
				for _, ex := range existing {
					tagIDs = append(tagIDs, ex.ID)
				}
				_ = e.Taxonomy.SetContentTaxonomies(item.ID, tagIDs)
			}
		}
	}

	logger.Info("Seeding completed",
		"settings", len(data.Settings),
		"categories", len(data.Categories),
		"tags", len(data.Tags),
		"contents", len(data.Contents),
	)
	return nil
}

// downloadSeedImage downloads a remote image to the local uploads directory,
// registers it in the media table, and returns the URL path (e.g. /static/uploads/demo/xxxx.jpg).
// A deduplication cache avoids downloading the same URL twice.
var seedImageCache = make(map[string]string)

func downloadSeedImage(remoteURL, uploadDir string, mediaRepo *coreMedia.Repository) (string, error) {
	// Dedup: same remote URL → same local path
	if local, ok := seedImageCache[remoteURL]; ok {
		return local, nil
	}

	demoDir := filepath.Join(uploadDir, "demo")
	if err := os.MkdirAll(demoDir, 0755); err != nil {
		return "", fmt.Errorf("create demo dir: %w", err)
	}

	// Generate random filename
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	filename := fmt.Sprintf("%x.jpg", b)
	fullPath := filepath.Join(demoDir, filename)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(remoteURL)
	if err != nil {
		return "", fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download returned status %d", resp.StatusCode)
	}

	// Detect extension from Content-Type
	ct := resp.Header.Get("Content-Type")
	mimeType := "image/jpeg"
	switch {
	case strings.Contains(ct, "png"):
		filename = strings.TrimSuffix(filename, ".jpg") + ".png"
		mimeType = "image/png"
	case strings.Contains(ct, "webp"):
		filename = strings.TrimSuffix(filename, ".jpg") + ".webp"
		mimeType = "image/webp"
	case strings.Contains(ct, "gif"):
		filename = strings.TrimSuffix(filename, ".jpg") + ".gif"
		mimeType = "image/gif"
	}
	fullPath = filepath.Join(demoDir, filename)

	dst, err := os.Create(fullPath)
	if err != nil {
		return "", err
	}
	defer dst.Close()

	written, err := io.Copy(dst, resp.Body)
	if err != nil {
		os.Remove(fullPath)
		return "", err
	}

	urlPath := fmt.Sprintf("/static/uploads/demo/%s", filename)

	// Register in media table
	if mediaRepo != nil {
		origName := filepath.Base(remoteURL)
		if idx := strings.Index(origName, "?"); idx > 0 {
			origName = origName[:idx]
		}
		m := &coreMedia.Media{
			Filename:     filename,
			OriginalName: origName,
			MimeType:     mimeType,
			Size:         written,
			Path:         urlPath,
			AltText:      "",
		}
		if err := mediaRepo.Create(m); err != nil {
			logger.Warn("Failed to register demo image in media table", "file", filename, "error", err)
		}
	}

	seedImageCache[remoteURL] = urlPath
	logger.Info("Downloaded seed image", "url", remoteURL, "local", urlPath)
	return urlPath, nil
}
