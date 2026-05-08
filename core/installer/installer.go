package installer

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"go-press/config"
	"go-press/core"
	"go-press/core/option"
	"go-press/core/user"
	"go-press/pkg/dbprefix"
	"go-press/pkg/logger"
	"go-press/pkg/middleware"
	pg "go-press/pkg/postgresql"
)

const (
	installerLanguageCookie = "gopress_install_lang"
)

func installerLanguage(c *gin.Context) string {
	if lang := strings.TrimSpace(c.Query("lang")); lang != "" {
		return normalizeInstallerLanguage(lang)
	}
	if cookie, err := c.Cookie(installerLanguageCookie); err == nil {
		return normalizeInstallerLanguage(cookie)
	}
	return defaultInstallerLanguage
}

type ActivationFunc func(cfg *config.Config, configPath string) error

type Installer struct {
	configPath    string
	initialConfig *config.Config
	bootError     string
	activate      ActivationFunc
	themeOptions  []ThemeOption
	templates     *template.Template
	router        *gin.Engine
}

type ThemeOption struct {
	Slug        string
	Name        string
	Description string
}

type pageData struct {
	Title        string
	Heading      string
	Lead         string
	Page         string
	Step         int
	Lang         string
	ConfigPath   string
	BootError    string
	Error        string
	Database     databaseValues
	Site         siteValues
	ThemeOptions []ThemeOption
}

type databaseValues struct {
	Database    string
	User        string
	Password    string
	Hostname    string
	Port        string
	Schema      string
	TablePrefix string
	ServerPort  int
}

type siteValues struct {
	SiteName      string
	SiteURL       string
	Tagline       string
	Language      string
	AdminLanguage string
	Theme         string
	AdminUsername string
	AdminEmail    string
}

type themeFile struct {
	Theme struct {
		Name        string `toml:"name"`
		Description string `toml:"description"`
	} `toml:"theme"`
}

func New(configPath string, initialConfig *config.Config, bootErr error, activate ActivationFunc) *Installer {
	i := &Installer{
		configPath:    configPath,
		initialConfig: cloneConfig(initialConfig),
		activate:      activate,
		themeOptions:  discoverThemes(),
	}
	if bootErr != nil {
		i.bootError = bootErr.Error()
	}

	funcMap := template.FuncMap{
		"T": installerT,
	}
	i.templates = template.Must(template.New("layout").Funcs(funcMap).Parse(layoutTemplate + welcomeTemplate + databaseTemplate + siteTemplate + successTemplate))

	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(middleware.SecurityHeaders())

	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":      "installing",
			"config_path": i.configPath,
			"time":        time.Now().Format(time.RFC3339),
		})
	})
	r.GET("/", func(c *gin.Context) {
		c.Redirect(http.StatusFound, "/install")
	})
	r.GET("/install", i.Welcome)
	r.POST("/install/language", i.LanguageSubmit)
	r.GET("/install/database", i.DatabasePage)
	r.POST("/install/database/test", i.DatabaseTest)
	r.POST("/install/database", i.DatabaseSubmit)
	r.GET("/install/site", i.SitePage)
	r.POST("/install/site", i.SiteSubmit)

	i.router = r
	return i
}

func (i *Installer) Router() http.Handler {
	return i.router
}

func (i *Installer) Welcome(c *gin.Context) {
	lang := installerLanguage(c)
	i.render(c, http.StatusOK, pageData{
		Title:      installerT(lang, "title.welcome"),
		Heading:    installerT(lang, "welcome.heading"),
		Lead:       installerT(lang, "welcome.lead"),
		Page:       "welcome",
		Lang:       lang,
		ConfigPath: i.configPath,
		BootError:  i.bootError,
	})
}

func (i *Installer) LanguageSubmit(c *gin.Context) {
	lang := normalizeInstallerLanguage(c.PostForm("language"))
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     installerLanguageCookie,
		Value:    lang,
		Path:     "/install",
		MaxAge:   3600,
		SameSite: http.SameSiteLaxMode,
	})
	c.Redirect(http.StatusSeeOther, "/install/database")
}

func (i *Installer) DatabasePage(c *gin.Context) {
	cfg := i.currentConfig()
	lang := installerLanguage(c)
	i.render(c, http.StatusOK, pageData{
		Title:      installerT(lang, "title.database"),
		Heading:    installerT(lang, "database.heading"),
		Lead:       installerT(lang, "database.lead"),
		Page:       "database",
		Step:       1,
		Lang:       lang,
		ConfigPath: i.configPath,
		BootError:  i.bootError,
		Database:   databaseValuesFromConfig(cfg),
	})
}

func (i *Installer) DatabaseTest(c *gin.Context) {
	lang := installerLanguage(c)
	cfg := i.currentConfig()
	cfg = applyDefaults(cfg)

	values := databaseValuesFromRequest(c, cfg)
	if err := validateDatabaseValues(values, lang); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"ok":    false,
			"error": err.Error(),
		})
		return
	}

	if err := applyDatabaseValues(cfg, values, lang); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"ok":    false,
			"error": err.Error(),
		})
		return
	}

	if err := pg.EnsureDatabase(cfg.PG); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"ok":    false,
			"error": installerT(lang, "error.database_verify_failed", err.Error()),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":      true,
		"message": installerT(lang, "database.test_success"),
	})
}

func (i *Installer) DatabaseSubmit(c *gin.Context) {
	lang := installerLanguage(c)
	cfg := i.currentConfig()
	cfg = applyDefaults(cfg)

	values := databaseValuesFromRequest(c, cfg)

	if err := validateDatabaseValues(values, lang); err != nil {
		i.render(c, http.StatusBadRequest, pageData{
			Title:      installerT(lang, "title.database"),
			Heading:    installerT(lang, "database.heading"),
			Lead:       installerT(lang, "database.required_lead"),
			Page:       "database",
			Step:       1,
			Lang:       lang,
			ConfigPath: i.configPath,
			BootError:  i.bootError,
			Error:      err.Error(),
			Database:   values,
		})
		return
	}

	if err := applyDatabaseValues(cfg, values, lang); err != nil {
		i.render(c, http.StatusInternalServerError, pageData{
			Title:      installerT(lang, "title.database"),
			Heading:    installerT(lang, "database.heading"),
			Lead:       installerT(lang, "database.lead"),
			Page:       "database",
			Step:       1,
			Lang:       lang,
			ConfigPath: i.configPath,
			BootError:  i.bootError,
			Error:      err.Error(),
			Database:   values,
		})
		return
	}

	if err := pg.EnsureDatabase(cfg.PG); err != nil {
		i.render(c, http.StatusBadGateway, pageData{
			Title:      installerT(lang, "title.database"),
			Heading:    installerT(lang, "database.heading"),
			Lead:       installerT(lang, "database.lead"),
			Page:       "database",
			Step:       1,
			Lang:       lang,
			ConfigPath: i.configPath,
			BootError:  i.bootError,
			Error:      installerT(lang, "error.database_verify_failed", err.Error()),
			Database:   values,
		})
		return
	}

	cfg.Install.Completed = false
	cfg.Install.InstalledAt = ""

	if err := config.Save(i.configPath, cfg); err != nil {
		i.render(c, http.StatusInternalServerError, pageData{
			Title:      installerT(lang, "title.database"),
			Heading:    installerT(lang, "database.heading"),
			Lead:       installerT(lang, "database.lead"),
			Page:       "database",
			Step:       1,
			Lang:       lang,
			ConfigPath: i.configPath,
			BootError:  i.bootError,
			Error:      installerT(lang, "error.config_write_failed", err.Error()),
			Database:   values,
		})
		return
	}

	c.Redirect(http.StatusSeeOther, "/install/site")
}

func (i *Installer) SitePage(c *gin.Context) {
	if !fileExists(i.configPath) {
		c.Redirect(http.StatusFound, "/install/database")
		return
	}

	cfg := i.currentConfig()
	lang := installerLanguage(c)
	site := siteValuesFromConfig(cfg, lang)
	i.render(c, http.StatusOK, pageData{
		Title:        installerT(lang, "title.site"),
		Heading:      installerT(lang, "site.heading"),
		Lead:         installerT(lang, "site.lead"),
		Page:         "site",
		Step:         2,
		Lang:         lang,
		ConfigPath:   i.configPath,
		BootError:    i.bootError,
		Site:         site,
		ThemeOptions: i.themeOptions,
	})
}

func (i *Installer) SiteSubmit(c *gin.Context) {
	if !fileExists(i.configPath) {
		c.Redirect(http.StatusFound, "/install/database")
		return
	}

	lang := installerLanguage(c)
	cfg := i.currentConfig()
	cfg = applyDefaults(cfg)
	adminLanguage := strings.TrimSpace(c.PostForm("admin_language"))
	if adminLanguage == "" {
		adminLanguage = lang
	}

	values := siteValues{
		SiteName:      strings.TrimSpace(c.PostForm("site_name")),
		SiteURL:       strings.TrimSpace(c.PostForm("site_url")),
		Tagline:       strings.TrimSpace(c.PostForm("tagline")),
		Language:      strings.TrimSpace(c.PostForm("language")),
		AdminLanguage: normalizeInstallerLanguage(adminLanguage),
		Theme:         strings.TrimSpace(c.PostForm("theme")),
		AdminUsername: strings.TrimSpace(c.PostForm("admin_username")),
		AdminEmail:    strings.TrimSpace(c.PostForm("admin_email")),
	}
	adminPassword := c.PostForm("admin_password")
	adminPasswordConfirm := c.PostForm("admin_password_confirm")

	if values.SiteName == "" || values.SiteURL == "" || values.Language == "" || values.Theme == "" || values.AdminUsername == "" || values.AdminEmail == "" || adminPassword == "" {
		i.render(c, http.StatusBadRequest, pageData{
			Title:        installerT(lang, "title.site"),
			Heading:      installerT(lang, "site.heading"),
			Lead:         installerT(lang, "site.lead"),
			Page:         "site",
			Step:         2,
			Lang:         lang,
			ConfigPath:   i.configPath,
			BootError:    i.bootError,
			Error:        installerT(lang, "error.site_required"),
			Site:         values,
			ThemeOptions: i.themeOptions,
		})
		return
	}

	if adminPassword != adminPasswordConfirm {
		i.render(c, http.StatusBadRequest, pageData{
			Title:        installerT(lang, "title.site"),
			Heading:      installerT(lang, "site.heading"),
			Lead:         installerT(lang, "site.lead"),
			Page:         "site",
			Step:         2,
			Lang:         lang,
			ConfigPath:   i.configPath,
			BootError:    i.bootError,
			Error:        installerT(lang, "error.password_mismatch"),
			Site:         values,
			ThemeOptions: i.themeOptions,
		})
		return
	}

	if !i.hasTheme(values.Theme) {
		i.render(c, http.StatusBadRequest, pageData{
			Title:        installerT(lang, "title.site"),
			Heading:      installerT(lang, "site.heading"),
			Lead:         installerT(lang, "site.lead"),
			Page:         "site",
			Step:         2,
			Lang:         lang,
			ConfigPath:   i.configPath,
			BootError:    i.bootError,
			Error:        installerT(lang, "error.theme_missing"),
			Site:         values,
			ThemeOptions: i.themeOptions,
		})
		return
	}

	cfg.Site.Name = values.SiteName
	cfg.Site.URL = values.SiteURL
	cfg.Site.Language = values.Language
	cfg.Site.Theme = values.Theme

	finalConfigPath, err := i.prepareSiteConfigPath(values.SiteURL, lang)
	if err != nil {
		i.render(c, http.StatusInternalServerError, pageData{
			Title:        installerT(lang, "title.site"),
			Heading:      installerT(lang, "site.heading"),
			Lead:         installerT(lang, "site.lead"),
			Page:         "site",
			Step:         2,
			Lang:         lang,
			ConfigPath:   i.configPath,
			BootError:    i.bootError,
			Error:        err.Error(),
			Site:         values,
			ThemeOptions: i.themeOptions,
		})
		return
	}
	i.configPath = finalConfigPath

	if err := config.Save(i.configPath, cfg); err != nil {
		i.render(c, http.StatusInternalServerError, pageData{
			Title:        installerT(lang, "title.site"),
			Heading:      installerT(lang, "site.heading"),
			Lead:         installerT(lang, "site.lead"),
			Page:         "site",
			Step:         2,
			Lang:         lang,
			ConfigPath:   i.configPath,
			BootError:    i.bootError,
			Error:        installerT(lang, "error.site_config_save_failed", err.Error()),
			Site:         values,
			ThemeOptions: i.themeOptions,
		})
		return
	}

	if err := i.initializeSite(cfg, values, adminPassword); err != nil {
		i.render(c, http.StatusInternalServerError, pageData{
			Title:        installerT(lang, "title.site"),
			Heading:      installerT(lang, "site.heading"),
			Lead:         installerT(lang, "site.lead"),
			Page:         "site",
			Step:         2,
			Lang:         lang,
			ConfigPath:   i.configPath,
			BootError:    i.bootError,
			Error:        err.Error(),
			Site:         values,
			ThemeOptions: i.themeOptions,
		})
		return
	}

	if i.activate != nil {
		if err := i.activate(cfg, i.configPath); err != nil {
			i.render(c, http.StatusInternalServerError, pageData{
				Title:      installerT(lang, "title.success"),
				Heading:    installerT(lang, "success.activate_failed_heading"),
				Lead:       installerT(lang, "success.activate_failed_lead"),
				Page:       "success",
				Step:       3,
				Lang:       lang,
				ConfigPath: i.configPath,
				Error:      err.Error(),
			})
			return
		}
	}

	i.render(c, http.StatusOK, pageData{
		Title:      installerT(lang, "title.success"),
		Heading:    installerT(lang, "success.heading"),
		Lead:       installerT(lang, "success.lead"),
		Page:       "success",
		Step:       3,
		Lang:       lang,
		ConfigPath: i.configPath,
	})
}

func (i *Installer) initializeSite(cfg *config.Config, values siteValues, adminPassword string) error {
	lang := normalizeInstallerLanguage(values.AdminLanguage)
	// Set table prefix before any DB operations
	if cfg.PG.TablePrefix != "" {
		dbprefix.Set(cfg.PG.TablePrefix)
	}

	db, err := pg.NewConnection(cfg.PG)
	if err != nil {
		return fmt.Errorf("%s: %w", installerT(lang, "error.target_database_connect_failed"), err)
	}
	defer closeDB(db)

	if err := core.MigrateDB(db); err != nil {
		return fmt.Errorf("%s: %w", installerT(lang, "error.migration_failed"), err)
	}

	userRepo := user.NewRepository(db)
	if _, err := userRepo.FindByUsername(values.AdminUsername); err == nil {
		return errors.New(installerT(lang, "error.admin_username_exists", values.AdminUsername))
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("%s: %w", installerT(lang, "error.admin_username_check_failed"), err)
	}

	if _, err := userRepo.FindByEmail(values.AdminEmail); err == nil {
		return errors.New(installerT(lang, "error.admin_email_exists", values.AdminEmail))
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("%s: %w", installerT(lang, "error.admin_email_check_failed"), err)
	}

	hash, err := user.HashPassword(adminPassword)
	if err != nil {
		return fmt.Errorf("%s: %w", installerT(lang, "error.admin_password_hash_failed"), err)
	}

	adminUser := &user.User{
		Username:     values.AdminUsername,
		Email:        values.AdminEmail,
		PasswordHash: hash,
		DisplayName:  values.AdminUsername,
		Role:         user.RoleSuperAdmin,
		IsActive:     true,
	}
	if err := userRepo.Create(adminUser); err != nil {
		return fmt.Errorf("%s: %w", installerT(lang, "error.admin_create_failed"), err)
	}

	store := option.NewStore(db)
	optionsToSet := map[string]string{
		"active_theme":     cfg.Site.Theme,
		"site_name":        values.SiteName,
		"site_description": values.Tagline,
		"site_language":    cfg.Site.Language,
		"admin_language":   normalizeInstallerLanguage(values.AdminLanguage),
		"admin_email":      values.AdminEmail,
	}
	if cfg.Site.Theme == "modern-company" {
		optionsToSet["company_name"] = values.SiteName
		optionsToSet["company_description"] = values.Tagline
		optionsToSet["company_email"] = values.AdminEmail
		optionsToSet["company_year"] = strconv.Itoa(time.Now().Year())
	}
	if cfg.Site.Theme == "financial-news" {
		optionsToSet["footer_text"] = fmt.Sprintf("© %d %s. Powered by GoPress.", time.Now().Year(), values.SiteName)
	}
	for key, value := range optionsToSet {
		if err := store.Set(key, value); err != nil {
			return fmt.Errorf("%s: %w", installerT(lang, "error.initial_setting_failed", key), err)
		}
	}

	cfg.Install.Completed = true
	cfg.Install.InstalledAt = time.Now().Format(time.RFC3339)
	return config.Save(i.configPath, cfg)
}

func (i *Installer) render(c *gin.Context, status int, data pageData) {
	data.Lang = normalizeInstallerLanguage(data.Lang)
	c.Status(status)
	c.Header("Content-Type", "text/html; charset=utf-8")
	if err := i.templates.ExecuteTemplate(c.Writer, "layout", data); err != nil {
		logger.Error("installer template render failed", "error", err)
		c.String(http.StatusInternalServerError, "installer template render failed")
	}
}

func (i *Installer) currentConfig() *config.Config {
	if fileExists(i.configPath) {
		cfg, err := config.Load(i.configPath)
		if err == nil {
			return applyDefaults(cfg)
		}
		logger.Warn("failed to load installer config, falling back to defaults", "path", i.configPath, "error", err)
	}
	return applyDefaults(cloneConfig(i.initialConfig))
}

func (i *Installer) hasTheme(slug string) bool {
	for _, theme := range i.themeOptions {
		if theme.Slug == slug {
			return true
		}
	}
	return false
}

func applyDefaults(cfg *config.Config) *config.Config {
	if cfg == nil {
		cfg = &config.Config{}
	}

	if cfg.Site.Name == "" {
		cfg.Site.Name = "My GoPress Site"
	}
	if cfg.Site.URL == "" {
		cfg.Site.URL = "http://localhost:8080"
	}
	if cfg.Site.Language == "" {
		cfg.Site.Language = "zh-CN"
	}
	if cfg.Site.Theme == "" {
		cfg.Site.Theme = "modern-company"
	}

	if cfg.Server.Host == "" {
		cfg.Server.Host = "0.0.0.0"
	}
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 8080
	}
	if cfg.Server.Mode == "" {
		cfg.Server.Mode = "debug"
	}

	if cfg.PG.Hostname == "" {
		cfg.PG.Hostname = "localhost"
	}
	if cfg.PG.Port == "" {
		cfg.PG.Port = "5432"
	}
	if cfg.PG.Schema == "" {
		cfg.PG.Schema = "public"
	}
	if cfg.PG.TablePrefix == "" {
		cfg.PG.TablePrefix = "gp_"
	}
	if cfg.PG.MaxOpenConns == 0 {
		cfg.PG.MaxOpenConns = 20
	}
	if cfg.PG.MaxIdleConns == 0 {
		cfg.PG.MaxIdleConns = 10
	}
	if cfg.PG.ConnMaxLifetime == "" {
		cfg.PG.ConnMaxLifetime = "30m"
	}

	if cfg.CMS.JWTExpireHours == 0 {
		cfg.CMS.JWTExpireHours = 24
	}
	if cfg.CMS.UploadDir == "" {
		cfg.CMS.UploadDir = "uploads"
	}
	if cfg.CMS.UploadMaxSizeMB == 0 {
		cfg.CMS.UploadMaxSizeMB = 10
	}

	return cfg
}

func databaseValuesFromConfig(cfg *config.Config) databaseValues {
	cfg = applyDefaults(cfg)
	return databaseValues{
		Database:    cfg.PG.Database,
		User:        cfg.PG.User,
		Password:    cfg.PG.Password,
		Hostname:    cfg.PG.Hostname,
		Port:        cfg.PG.Port,
		Schema:      cfg.PG.Schema,
		TablePrefix: cfg.PG.TablePrefix,
		ServerPort:  cfg.Server.Port,
	}
}

func siteValuesFromConfig(cfg *config.Config, installerLang string) siteValues {
	cfg = applyDefaults(cfg)
	installerLang = normalizeInstallerLanguage(installerLang)
	return siteValues{
		SiteName:      cfg.Site.Name,
		SiteURL:       cfg.Site.URL,
		Language:      installerLang,
		AdminLanguage: installerLang,
		Theme:         cfg.Site.Theme,
		AdminUsername: "admin",
		AdminEmail:    "admin@example.com",
	}
}

func cloneConfig(cfg *config.Config) *config.Config {
	if cfg == nil {
		return nil
	}
	copyCfg := *cfg
	return &copyCfg
}

func discoverThemes() []ThemeOption {
	entries, err := os.ReadDir("themes")
	if err != nil {
		return []ThemeOption{{Slug: "modern-company", Name: "modern-company"}}
	}

	var themes []ThemeOption
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		slug := entry.Name()
		metaPath := filepath.Join("themes", slug, "theme.toml")
		var meta themeFile
		if _, err := toml.DecodeFile(metaPath, &meta); err != nil {
			themes = append(themes, ThemeOption{Slug: slug, Name: slug})
			continue
		}

		name := meta.Theme.Name
		if name == "" {
			name = slug
		}
		themes = append(themes, ThemeOption{
			Slug:        slug,
			Name:        name,
			Description: meta.Theme.Description,
		})
	}

	sort.Slice(themes, func(i, j int) bool {
		return themes[i].Slug < themes[j].Slug
	})

	if len(themes) == 0 {
		return []ThemeOption{{Slug: "modern-company", Name: "modern-company"}}
	}
	return themes
}

func generateSecret(bytesLen int) (string, error) {
	raw := make([]byte, bytesLen)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

func parsePortOrDefault(value string, fallback int) int {
	port, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || port < 1 || port > 65535 {
		return fallback
	}
	return port
}

func databaseValuesFromRequest(c *gin.Context, fallback *config.Config) databaseValues {
	tablePrefix := strings.TrimSpace(c.PostForm("table_prefix"))
	if tablePrefix == "" {
		tablePrefix = "gp_"
	}
	return databaseValues{
		Database:    strings.TrimSpace(c.PostForm("database")),
		User:        strings.TrimSpace(c.PostForm("user")),
		Password:    c.PostForm("password"),
		Hostname:    strings.TrimSpace(c.PostForm("host")),
		Port:        strings.TrimSpace(c.PostForm("port")),
		Schema:      strings.TrimSpace(c.PostForm("schema")),
		TablePrefix: tablePrefix,
		ServerPort:  parsePortOrDefault(c.PostForm("server_port"), fallback.Server.Port),
	}
}

func validateDatabaseValues(values databaseValues, lang string) error {
	if values.Database == "" || values.User == "" || values.Password == "" || values.Hostname == "" || values.Port == "" || values.Schema == "" {
		return errors.New(installerT(lang, "error.database_required"))
	}
	return nil
}

func applyDatabaseValues(cfg *config.Config, values databaseValues, lang string) error {
	cfg.PG.Database = values.Database
	cfg.PG.User = values.User
	cfg.PG.Password = values.Password
	cfg.PG.Hostname = values.Hostname
	cfg.PG.Port = values.Port
	cfg.PG.Schema = values.Schema
	cfg.PG.TablePrefix = values.TablePrefix
	cfg.Server.Port = values.ServerPort

	if cfg.CMS.JWTSecret == "" {
		secret, err := generateSecret(32)
		if err != nil {
			return fmt.Errorf("%s: %w", installerT(lang, "error.jwt_secret_failed"), err)
		}
		cfg.CMS.JWTSecret = secret
	}

	return nil
}

func (i *Installer) prepareSiteConfigPath(siteURL string, langs ...string) (string, error) {
	lang := defaultInstallerLanguage
	if len(langs) > 0 {
		lang = normalizeInstallerLanguage(langs[0])
	}
	currentDir := filepath.Dir(i.configPath)
	sitesRoot := filepath.Dir(currentDir)

	if filepath.Base(currentDir) != "default" || filepath.Base(sitesRoot) != "sites" {
		return i.configPath, nil
	}

	siteDirName, err := siteDirNameFromURL(siteURL)
	if err != nil {
		return "", fmt.Errorf("%s: %w", installerT(lang, "error.site_dir_from_url_failed"), err)
	}

	targetDir := filepath.Join(sitesRoot, siteDirName)
	if targetDir == currentDir {
		return filepath.Join(targetDir, "config.toml"), nil
	}

	if _, err := os.Stat(targetDir); err == nil {
		return "", errors.New(installerT(lang, "error.site_dir_exists", targetDir))
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("%s: %w", installerT(lang, "error.site_dir_check_failed"), err)
	}

	if err := os.Rename(currentDir, targetDir); err != nil {
		return "", fmt.Errorf("%s: %w", installerT(lang, "error.site_dir_rename_failed"), err)
	}

	return filepath.Join(targetDir, "config.toml"), nil
}

func siteDirNameFromURL(siteURL string) (string, error) {
	raw := strings.TrimSpace(siteURL)
	if raw == "" {
		return "", fmt.Errorf("site URL is required")
	}

	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" {
		parsed, err = url.Parse("https://" + raw)
		if err != nil {
			return "", err
		}
	}

	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	if host == "" {
		return "", fmt.Errorf("could not parse site hostname")
	}

	var b strings.Builder
	lastHyphen := false
	for _, r := range host {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
			lastHyphen = false
		case r >= '0' && r <= '9':
			b.WriteRune(r)
			lastHyphen = false
		default:
			if !lastHyphen {
				b.WriteByte('-')
				lastHyphen = true
			}
		}
	}

	name := strings.Trim(b.String(), "-")
	if name == "" {
		return "", fmt.Errorf("site URL %q cannot produce a valid directory name", siteURL)
	}

	return name, nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func closeDB(db *gorm.DB) {
	sqlDB, err := db.DB()
	if err != nil {
		return
	}
	_ = sqlDB.Close()
}
