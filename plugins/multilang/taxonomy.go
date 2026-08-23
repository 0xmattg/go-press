package multilang

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/0xmattg/go-press/core/admin"
	"github.com/0xmattg/go-press/core/content"
	"github.com/0xmattg/go-press/core/hook"
	"github.com/0xmattg/go-press/core/rewrite"
	"github.com/0xmattg/go-press/core/taxonomy"
	"github.com/0xmattg/go-press/pkg/dbprefix"
)

const (
	taxonomyModeShared         = "shared"
	taxonomyModeTranslatedOnly = "translated_only"
)

var supportedTranslatedTaxonomies = []string{"category", "tag"}

type skipTaxonomyAutoLinkKey struct{}

type TaxonomyPolicyView struct {
	Type  string
	Label string
	Mode  string
}

type TaxonomyTranslationView struct {
	ID           uint
	Type         string
	TypeLabel    string
	AdminSlug    string
	Name         string
	Slug         string
	Description  string
	LanguageCode string
	Translations map[string]uint
}

func taxonomyModeOption(taxonomyType string) string {
	return "plugin_multi-language_taxonomy_" + taxonomyType + "_mode"
}

func (p *Plugin) taxonomyMode(taxonomyType string) string {
	if p == nil || p.engine == nil || p.engine.Options == nil {
		return taxonomyModeShared
	}
	if value := strings.TrimSpace(p.engine.Options.Get(taxonomyModeOption(taxonomyType))); value == taxonomyModeTranslatedOnly {
		return value
	}
	return taxonomyModeShared
}

func (p *Plugin) translatedTaxonomyTypes() []string {
	if p == nil || p.engine == nil || p.engine.Registry == nil {
		return nil
	}
	var types []string
	for _, taxonomyType := range supportedTranslatedTaxonomies {
		if p.taxonomyMode(taxonomyType) == taxonomyModeTranslatedOnly && p.engine.Registry.GetTaxonomy(taxonomyType) != nil {
			types = append(types, taxonomyType)
		}
	}
	return types
}

func (p *Plugin) registerLangTaxonomyScope(c *gin.Context, lang string) {
	lang = strings.ToLower(strings.TrimSpace(lang))
	enabled := p.translatedTaxonomyTypes()
	if c == nil || len(enabled) == 0 || strings.TrimSpace(lang) == "" {
		return
	}
	defaultLang := p.getDefaultLang()
	taxTable := dbprefix.Table("taxonomies")
	translationTable := TaxonomyTranslation{}.TableName()
	taxonomy.AddScope(c, taxonomy.Scope{
		Key: lang,
		Apply: func(db *gorm.DB) *gorm.DB {
			if lang == defaultLang {
				return db.Where(
					taxTable+".taxonomy NOT IN ? OR "+taxTable+".id IN (SELECT taxonomy_id FROM "+translationTable+" WHERE language_code = ?) OR "+taxTable+".id NOT IN (SELECT taxonomy_id FROM "+translationTable+")",
					enabled, lang,
				)
			}
			return db.Where(
				taxTable+".taxonomy NOT IN ? OR "+taxTable+".id IN (SELECT taxonomy_id FROM "+translationTable+" WHERE language_code = ?)",
				enabled, lang,
			)
		},
	})
}

func (p *Plugin) taxonomyContext(lang string, skipAutoLink bool) context.Context {
	c := &gin.Context{}
	p.registerLangTaxonomyScope(c, lang)
	ctx := taxonomy.RequestContext(c)
	if skipAutoLink {
		ctx = context.WithValue(ctx, skipTaxonomyAutoLinkKey{}, true)
	}
	return ctx
}

func (p *Plugin) registerTaxonomyHooks() {
	if p.engine == nil || p.engine.Hooks == nil {
		return
	}
	p.hookHandles = append(p.hookHandles,
		p.engine.Hooks.AddFilter(admin.HookTaxonomyListTabs, p.adminTaxonomyListTabs, 10),
		p.engine.Hooks.AddFilter(hook.SEOTaxonomyMeta, p.taxonomySEOMeta, 10),
		p.engine.Hooks.AddAction(hook.TaxonomyCreated, func(ctx context.Context, args ...interface{}) {
			if skipped, _ := ctx.Value(skipTaxonomyAutoLinkKey{}).(bool); skipped || len(args) == 0 {
				return
			}
			item, _ := args[0].(*taxonomy.Taxonomy)
			lang := taxonomy.ScopeKey(ctx)
			if item == nil || lang == "" || lang == p.getDefaultLang() || p.taxonomyMode(item.Taxonomy) != taxonomyModeTranslatedOnly {
				return
			}
			_, _ = p.repo.EnsureTaxonomyTranslation(item.Taxonomy, item.ID, lang, lang, item.ID)
		}, 10),
		p.engine.Hooks.AddAction(hook.TaxonomyDeleted, func(_ context.Context, args ...interface{}) {
			if len(args) == 0 {
				return
			}
			if item, ok := args[0].(*taxonomy.Taxonomy); ok && item != nil {
				_ = p.repo.DeleteTaxonomyTranslation(item.ID)
			}
		}, 10),
	)
}

func (p *Plugin) adminTaxonomyListTabs(value interface{}, args ...interface{}) interface{} {
	tabs, _ := value.([]admin.ContentListTab)
	if p.repo == nil || len(args) < 2 {
		return tabs
	}
	c, _ := args[0].(*gin.Context)
	taxonomyType, _ := args[1].(string)
	if c == nil || p.taxonomyMode(taxonomyType) != taxonomyModeTranslatedOnly {
		return tabs
	}
	languages, err := p.repo.ActiveLanguages()
	if err != nil || len(languages) < 2 {
		return tabs
	}
	active := c.Query("lang")
	base := c.Request.URL.Path
	tabs = append(tabs, admin.ContentListTab{Key: "all", Label: "全部", Count: p.countTaxonomy(taxonomyType, ""), Active: active == "", URL: base})
	for _, language := range languages {
		label := strings.TrimSpace(language.Flag + " " + language.Name)
		tabs = append(tabs, admin.ContentListTab{
			Key: language.Code, Label: label, Count: p.countTaxonomy(taxonomyType, language.Code),
			Active: active == language.Code, URL: base + "?lang=" + url.QueryEscape(language.Code),
		})
	}
	return tabs
}

func (p *Plugin) countTaxonomy(taxonomyType, lang string) int {
	if p.engine == nil || p.engine.Taxonomy == nil {
		return 0
	}
	ctx := context.Background()
	if lang != "" {
		ctx = p.taxonomyContext(lang, false)
	}
	items, err := p.engine.Taxonomy.ListByTaxonomyContext(ctx, taxonomyType)
	if err != nil {
		return 0
	}
	return len(items)
}

func (p *Plugin) taxonomySettingsData(data map[string]interface{}, languages []Language, defaultLang string) {
	if data == nil || p.engine == nil || p.engine.Taxonomy == nil {
		return
	}
	policies := make([]TaxonomyPolicyView, 0, len(supportedTranslatedTaxonomies))
	for _, taxonomyType := range supportedTranslatedTaxonomies {
		label := taxonomyType
		if definition := p.engine.Registry.GetTaxonomy(taxonomyType); definition != nil {
			label = definition.LabelPlural
		}
		policies = append(policies, TaxonomyPolicyView{Type: taxonomyType, Label: label, Mode: p.taxonomyMode(taxonomyType)})
	}
	data["TaxonomyPolicies"] = policies
	data["HasTranslatedTaxonomies"] = len(p.translatedTaxonomyTypes()) > 0

	links, _ := p.repo.ListTaxonomyTranslations()
	byTaxonomy := make(map[uint]TaxonomyTranslation, len(links))
	byGroup := make(map[uint]map[string]uint)
	for _, link := range links {
		byTaxonomy[link.TaxonomyID] = link
		if byGroup[link.GroupID] == nil {
			byGroup[link.GroupID] = make(map[string]uint)
		}
		byGroup[link.GroupID][link.LanguageCode] = link.TaxonomyID
	}
	seenGroups := make(map[uint]bool)
	var views []TaxonomyTranslationView
	for _, taxonomyType := range supportedTranslatedTaxonomies {
		if p.taxonomyMode(taxonomyType) != taxonomyModeTranslatedOnly {
			continue
		}
		items, err := p.engine.Taxonomy.ListByTaxonomy(taxonomyType)
		if err != nil {
			continue
		}
		for _, item := range items {
			link, linked := byTaxonomy[item.ID]
			lang := defaultLang
			translations := map[string]uint{defaultLang: item.ID}
			if linked {
				if seenGroups[link.GroupID] {
					continue
				}
				seenGroups[link.GroupID] = true
				lang = link.LanguageCode
				translations = byGroup[link.GroupID]
				if defaultID := translations[defaultLang]; defaultID != 0 && defaultID != item.ID {
					if defaultItem, loadErr := p.engine.Taxonomy.GetTaxonomy(defaultID); loadErr == nil {
						item = *defaultItem
						lang = defaultLang
					}
				}
			}
			label := taxonomyType
			if definition := p.engine.Registry.GetTaxonomy(taxonomyType); definition != nil {
				label = definition.Label
			}
			views = append(views, TaxonomyTranslationView{
				ID: item.ID, Type: taxonomyType, TypeLabel: label, AdminSlug: admin.AdminSlug(taxonomyType), Name: item.Term.Name,
				Slug: item.Term.Slug, Description: item.Description,
				LanguageCode: lang, Translations: translations,
			})
		}
	}
	data["TaxonomyItems"] = views
	data["TaxonomyModesEnabled"] = len(languages) > 1
}

func (p *Plugin) handleCreateTaxonomyTranslation(c *gin.Context) {
	redirect := func(key, message string) {
		c.Redirect(http.StatusFound, "/admin/plugins/multi-language/settings?"+key+"="+url.QueryEscape(message)+"#taxonomy-translations")
	}
	sourceID64, err := strconv.ParseUint(c.PostForm("taxonomy_id"), 10, 64)
	targetLang := strings.ToLower(strings.TrimSpace(c.PostForm("target_lang")))
	role := c.GetString("admin_role")
	if p.engine == nil || p.engine.RBAC == nil || !p.engine.RBAC.Can(role, "taxonomy", "read") || !p.engine.RBAC.Can(role, "taxonomy", "create") {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}
	if err != nil || sourceID64 == 0 || p.repo == nil || !p.isSupported(targetLang) {
		redirect("error", "分类或标签翻译参数无效")
		return
	}
	source, err := p.engine.Taxonomy.GetTaxonomy(uint(sourceID64))
	if err != nil || source == nil || p.taxonomyMode(source.Taxonomy) != taxonomyModeTranslatedOnly {
		redirect("error", "分类或标签项不存在，或尚未启用独立翻译")
		return
	}
	sourceLang := p.getDefaultLang()
	sourceWasLinked := false
	if existing, existingErr := p.repo.GetTaxonomyTranslation(source.ID); existingErr == nil {
		sourceWasLinked = true
		sourceLang = existing.LanguageCode
		if _, duplicateErr := p.repo.FindTaxonomyTranslation(existing.GroupID, targetLang); duplicateErr == nil {
			redirect("error", "该语言的分类翻译已存在")
			return
		}
	}
	if sourceLang == targetLang {
		redirect("error", "目标语言与来源语言相同")
		return
	}
	var parentID *uint
	if source.ParentID != nil {
		parentLink, parentErr := p.repo.GetTaxonomyTranslation(*source.ParentID)
		if parentErr != nil {
			redirect("error", "请先为当前项的父级创建翻译")
			return
		}
		targetParent, parentErr := p.repo.FindTaxonomyTranslation(parentLink.GroupID, targetLang)
		if parentErr != nil {
			redirect("error", "请先为当前项的父级创建目标语言翻译")
			return
		}
		parentID = &targetParent.TaxonomyID
	}
	sourceLink, err := p.repo.EnsureTaxonomyTranslation(source.Taxonomy, source.ID, sourceLang, sourceLang, source.ID)
	if err != nil {
		redirect("error", "创建分类或标签翻译组失败: "+err.Error())
		return
	}

	name := strings.TrimSpace(c.PostForm("name"))
	slug := strings.Trim(strings.TrimSpace(c.PostForm("slug")), "/")
	if name == "" {
		name = source.Term.Name + " [" + targetLang + "]"
	}
	if slug == "" {
		slug = source.Term.Slug
	}
	target := &taxonomy.Taxonomy{
		Taxonomy: source.Taxonomy, ParentID: parentID,
		Description: strings.TrimSpace(c.PostForm("description")),
		Term:        taxonomy.Term{Name: name, Slug: slug},
	}
	targetCtx := p.taxonomyContext(targetLang, true)
	if err := p.engine.TaxonomyCommands.Create(targetCtx, target); err != nil {
		if !sourceWasLinked {
			_ = p.repo.DeleteTaxonomyTranslation(source.ID)
		}
		redirect("error", "创建分类或标签翻译失败: "+err.Error())
		return
	}
	if err := p.repo.LinkTaxonomyTranslation(sourceLink.GroupID, target.ID, targetLang, sourceLang); err != nil {
		_ = p.engine.TaxonomyCommands.Delete(targetCtx, target.Taxonomy, target.ID)
		if !sourceWasLinked {
			_ = p.repo.DeleteTaxonomyTranslation(source.ID)
		}
		redirect("error", "关联分类或标签翻译失败: "+err.Error())
		return
	}
	p.reconcileTaxonomyRelationships(source.ID, target.ID, targetLang)
	redirect("success", "已创建 "+targetLang+" 分类或标签翻译")
}

func (p *Plugin) reconcileTaxonomyRelationships(sourceTaxonomyID, targetTaxonomyID uint, targetLang string) {
	if p.engine == nil || p.engine.DB == nil || p.engine.TaxonomyCommands == nil {
		return
	}
	var contentIDs []uint
	_ = p.engine.DB.Model(&taxonomy.TermRelationship{}).Where("taxonomy_id = ?", sourceTaxonomyID).Pluck("content_id", &contentIDs).Error
	ctx := p.taxonomyContext(targetLang, true)
	for _, sourceContentID := range contentIDs {
		translation, err := p.repo.GetTranslation(sourceContentID)
		if err != nil {
			continue
		}
		targetContentID, err := p.repo.GetTranslatedContentID(translation.Trid, targetLang)
		if err != nil {
			continue
		}
		targetContent, err := p.engine.Content.FindByID(targetContentID)
		if err != nil {
			continue
		}
		definition := p.engine.Registry.GetType(targetContent.Type)
		if definition == nil {
			continue
		}
		items, _ := p.engine.Taxonomy.GetContentTaxonomiesContext(ctx, targetContentID, "")
		ids := make([]uint, 0, len(items)+1)
		for _, item := range items {
			if item.ID != targetTaxonomyID {
				ids = append(ids, item.ID)
			}
		}
		ids = append(ids, targetTaxonomyID)
		_ = p.engine.TaxonomyCommands.SetContentTaxonomies(ctx, targetContentID, definition.Taxonomies, ids)
	}
}

func (p *Plugin) copyTranslatedTaxonomies(sourceContentID, targetContentID uint, targetLang string, definition *content.ContentTypeDef) error {
	if definition == nil || p.engine == nil || p.engine.TaxonomyCommands == nil {
		return nil
	}
	sourceItems, err := p.engine.Taxonomy.GetContentTaxonomies(sourceContentID, "")
	if err != nil {
		return err
	}
	ids := make([]uint, 0, len(sourceItems))
	for _, source := range sourceItems {
		if p.taxonomyMode(source.Taxonomy) != taxonomyModeTranslatedOnly {
			ids = append(ids, source.ID)
			continue
		}
		link, linkErr := p.repo.GetTaxonomyTranslation(source.ID)
		if linkErr != nil {
			continue
		}
		target, targetErr := p.repo.FindTaxonomyTranslation(link.GroupID, targetLang)
		if targetErr == nil {
			ids = append(ids, target.TaxonomyID)
		}
	}
	return p.engine.TaxonomyCommands.SetContentTaxonomies(p.taxonomyContext(targetLang, true), targetContentID, definition.Taxonomies, ids)
}

func (p *Plugin) resolveTaxonomyTranslation(path, sourceLang, targetLang string) (string, bool) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 2 || p.taxonomyMode(parts[0]) != taxonomyModeTranslatedOnly || p.engine == nil {
		return "", false
	}
	source, err := p.engine.Taxonomy.FindByTypeAndSlugContext(p.taxonomyContext(sourceLang, false), parts[0], parts[1])
	if err != nil {
		return "", true
	}
	link, err := p.repo.GetTaxonomyTranslation(source.ID)
	if err != nil {
		if sourceLang == targetLang || targetLang == p.getDefaultLang() {
			return p.prefixedTaxonomyPath(targetLang, source.Taxonomy, source.Term.Slug), true
		}
		return "", true
	}
	targetLink, err := p.repo.FindTaxonomyTranslation(link.GroupID, targetLang)
	if err != nil {
		return "", true
	}
	target, err := p.engine.Taxonomy.GetTaxonomy(targetLink.TaxonomyID)
	if err != nil || target.Taxonomy != source.Taxonomy {
		return "", true
	}
	return p.prefixedTaxonomyPath(targetLang, target.Taxonomy, target.Term.Slug), true
}

func (p *Plugin) prefixedTaxonomyPath(lang, taxonomyType, slug string) string {
	path := rewrite.BuildTaxonomyURL(taxonomyType, slug)
	if lang == p.getDefaultLang() {
		return path
	}
	return "/" + lang + path
}

func (p *Plugin) redirectLegacyTaxonomyAlias(c *gin.Context, originalPath, lang string, hadLanguagePrefix bool) bool {
	if c == nil || c.Request == nil || (c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead) {
		return false
	}
	parts := strings.Split(strings.Trim(originalPath, "/"), "/")
	if len(parts) != 2 || p.taxonomyMode(parts[0]) != taxonomyModeTranslatedOnly {
		return false
	}
	if current, err := p.engine.Taxonomy.FindByTypeAndSlugContext(p.taxonomyContext(lang, false), parts[0], parts[1]); err == nil {
		if lang == p.getDefaultLang() || hadLanguagePrefix {
			return false
		}
		destination := p.prefixedTaxonomyPath(lang, current.Taxonomy, current.Term.Slug)
		if c.Request.URL.RawQuery != "" {
			destination += "?" + c.Request.URL.RawQuery
		}
		c.Redirect(http.StatusMovedPermanently, destination)
		c.Abort()
		return true
	}
	source, err := p.engine.Taxonomy.FindByTypeAndSlugContext(p.taxonomyContext(p.getDefaultLang(), false), parts[0], parts[1])
	if err != nil {
		return false
	}
	link, err := p.repo.GetTaxonomyTranslation(source.ID)
	if err != nil {
		return false
	}
	targetLink, err := p.repo.FindTaxonomyTranslation(link.GroupID, lang)
	if err != nil {
		return false
	}
	target, err := p.engine.Taxonomy.GetTaxonomy(targetLink.TaxonomyID)
	if err != nil {
		return false
	}
	destination := p.prefixedTaxonomyPath(lang, target.Taxonomy, target.Term.Slug)
	if c.Request.URL.RawQuery != "" {
		destination += "?" + c.Request.URL.RawQuery
	}
	c.Redirect(http.StatusMovedPermanently, destination)
	c.Abort()
	return true
}

func (p *Plugin) taxonomySEOMeta(value interface{}, args ...interface{}) interface{} {
	meta, ok := value.(rewrite.SEOMeta)
	if !ok || len(args) < 2 || p.engine == nil || p.engine.Sitemap == nil {
		return value
	}
	item, _ := args[1].(*taxonomy.Taxonomy)
	if item == nil || p.taxonomyMode(item.Taxonomy) != taxonomyModeTranslatedOnly {
		return meta
	}
	link, err := p.repo.GetTaxonomyTranslation(item.ID)
	if err != nil {
		return meta
	}
	siblings, err := p.repo.GetTaxonomyTranslationsByGroup(link.GroupID)
	if err != nil {
		return meta
	}
	siteURL := strings.TrimRight(p.engine.Sitemap.SiteURL(), "/")
	var alternates []rewrite.SEOAlternate
	for _, sibling := range siblings {
		target, loadErr := p.engine.Taxonomy.GetTaxonomy(sibling.TaxonomyID)
		if loadErr != nil {
			continue
		}
		alternates = append(alternates, rewrite.SEOAlternate{HrefLang: sibling.LanguageCode, Href: siteURL + p.prefixedTaxonomyPath(sibling.LanguageCode, target.Taxonomy, target.Term.Slug)})
		if sibling.LanguageCode == p.getDefaultLang() {
			alternates = append(alternates, rewrite.SEOAlternate{HrefLang: "x-default", Href: siteURL + p.prefixedTaxonomyPath(sibling.LanguageCode, target.Taxonomy, target.Term.Slug)})
		}
	}
	meta.Alternates = alternates
	return meta
}

func (p *Plugin) validateTaxonomyTranslationState() error {
	if p.repo == nil {
		return nil
	}
	count, err := p.repo.CountTaxonomyTranslations()
	if err != nil {
		return err
	}
	if count > 0 {
		return errors.New("仍存在分类或标签翻译数据，请先将其迁移或删除后再停用多语言插件")
	}
	return nil
}
