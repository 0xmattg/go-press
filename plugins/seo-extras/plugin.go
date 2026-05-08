// Package seoextras adds Yoast-style per-content SEO override fields without
// any change to GoPress core. Activating the plugin gives every content edit
// page (products / services / posts / etc.) an "SEO settings" meta box where
// authors can override the auto-generated <title>, <meta description>,
// og:image and robots directive on a per-row basis. Deactivate to remove the
// meta box and revert to default SEOBuilder output — stored overrides remain
// in gp_content_meta but stop being read.
//
// Architecture: the plugin is a pure consumer of three public extension
// points exposed by core in this branch — admin.content_form.fields (renders
// the meta box), admin.content.saved (persists the form values), and
// seo.content.meta (patches the SEOMeta before the front-end template
// renders it). Core knows nothing about SEO overrides; another plugin could
// reuse the same hooks for, say, schema.org Product fields.
package seoextras

import (
	"context"
	"fmt"
	"html/template"
	"strings"

	"github.com/gin-gonic/gin"

	"go-press/core"
	"go-press/core/content"
	"go-press/core/hook"
	"go-press/core/plugin"
	"go-press/core/rewrite"
	"go-press/pkg/logger"
)

const (
	PluginName = "seo-extras"

	// Meta keys persisted to gp_content_meta. Prefix `_seo_` keeps them out
	// of the regular meta surface (typeDef.MetaFields) and clearly attributed
	// to this plugin.
	metaKeyTitle       = "_seo_title"
	metaKeyDescription = "_seo_description"
	metaKeyImage       = "_seo_image"
	metaKeyRobots      = "_seo_robots"
)

// Plugin implements plugin.Plugin (no settings page — all state is per-content).
type Plugin struct {
	engine *core.Engine

	// Hook handles for clean Deactivate; without these, leftover hooks would
	// keep firing after the plugin is disabled, leaving phantom meta boxes
	// and SEO overrides.
	hookHandles []hook.Handle
}

// New constructs a fresh Plugin.
func New() *Plugin { return &Plugin{} }

// --- Plugin interface ---

func (p *Plugin) Name() string    { return PluginName }
func (p *Plugin) Version() string { return "1.0.0" }
func (p *Plugin) Description() string {
	return "Yoast 风格的内容级 SEO 覆盖：在每条内容编辑页提供独立的 SEO Title / Description / Open Graph 图片 / Robots 字段"
}

// Activate wires up the three hooks. All custom UI / save / render behavior
// lives behind these hooks — no routes, no DB tables, no settings page.
func (p *Plugin) Activate(app plugin.App) {
	e, ok := app.(*core.Engine)
	if !ok {
		logger.Error("seo-extras: failed to cast app to *core.Engine")
		return
	}
	p.engine = e
	p.hookHandles = p.hookHandles[:0]

	// 1. Render the SEO meta box on every content edit/create form.
	p.hookHandles = append(p.hookHandles,
		e.Hooks.AddFilter(hook.AdminContentFormFields, p.renderMetaBox, 50))

	// 2. Persist form values when the admin saves a content row.
	p.hookHandles = append(p.hookHandles,
		e.Hooks.AddAction(hook.AdminContentSaved, p.saveFields, 50))

	// 3. Override SEOMeta on the front-end render pipeline.
	p.hookHandles = append(p.hookHandles,
		e.Hooks.AddFilter(hook.SEOContentMeta, p.applyOverrides, 50))

	logger.Info("seo-extras plugin activated")
}

// Deactivate cleanly removes every hook so the plugin is a runtime no-op
// without restarting the server. Stored overrides in gp_content_meta are
// preserved but no longer read.
func (p *Plugin) Deactivate(app plugin.App) {
	if p.engine == nil {
		return
	}
	for _, h := range p.hookHandles {
		// h might be an action or filter handle; bus methods are forgiving.
		p.engine.Hooks.RemoveAction(h)
		p.engine.Hooks.RemoveFilter(h)
	}
	p.hookHandles = p.hookHandles[:0]
	logger.Info("seo-extras plugin deactivated")
}

// renderMetaBox returns the meta box HTML appended to whatever earlier
// filters returned. Filter args are: (*content.Content, *content.ContentTypeDef).
// On the create form .Item is nil, so we treat metaMap as empty — the form
// renders with all blanks and saveFields handles the first persist normally.
func (p *Plugin) renderMetaBox(value interface{}, args ...interface{}) interface{} {
	if p.engine == nil || !p.engine.PluginManager.IsActive(PluginName) {
		return value
	}

	existing := template.HTML("")
	switch v := value.(type) {
	case template.HTML:
		existing = v
	case string:
		existing = template.HTML(v)
	}

	// args[0] may be *content.Content (edit) or nil (create). Pre-fill the
	// form with stored values when editing.
	var meta map[string]string
	if len(args) > 0 {
		if item, ok := args[0].(*content.Content); ok && item != nil && item.ID != 0 {
			meta, _ = p.engine.Content.GetMeta(item.ID)
		}
	}

	return existing + buildMetaBoxHTML(meta)
}

// saveFields writes the four SEO override fields to gp_content_meta. Empty
// values are deleted rather than stored as "" so the SEOContentMeta filter
// sees a clean absence and falls through to the default SEOBuilder output.
// Args: (*gin.Context, *content.Content).
func (p *Plugin) saveFields(_ context.Context, args ...interface{}) {
	if p.engine == nil || !p.engine.PluginManager.IsActive(PluginName) {
		return
	}
	if len(args) < 2 {
		return
	}
	c, ok1 := args[0].(*gin.Context)
	item, ok2 := args[1].(*content.Content)
	if !ok1 || !ok2 || c == nil || item == nil || item.ID == 0 {
		return
	}

	pairs := []struct {
		key   string
		value string
	}{
		{metaKeyTitle, strings.TrimSpace(c.PostForm("_seo_title"))},
		{metaKeyDescription, strings.TrimSpace(c.PostForm("_seo_description"))},
		{metaKeyImage, strings.TrimSpace(c.PostForm("_seo_image"))},
		{metaKeyRobots, strings.TrimSpace(c.PostForm("_seo_robots"))},
	}
	for _, kv := range pairs {
		if kv.value == "" {
			// DeleteMeta keeps absence unambiguous: the read path treats
			// missing keys as "use default", so storing empty strings would
			// just make later reads ambiguous.
			_ = p.engine.Content.DeleteMeta(item.ID, kv.key)
		} else {
			_ = p.engine.Content.SaveMeta(item.ID, kv.key, kv.value)
		}
	}
}

// applyOverrides patches SEOMeta with per-content overrides when present.
// Only non-empty meta values take effect, so default Excerpt/ImageURL flow
// is preserved for any field the author leaves blank.
// Filter value: rewrite.SEOMeta. Args: (*content.Content, map[string]string).
func (p *Plugin) applyOverrides(value interface{}, args ...interface{}) interface{} {
	if p.engine == nil || !p.engine.PluginManager.IsActive(PluginName) {
		return value
	}
	seo, ok := value.(rewrite.SEOMeta)
	if !ok {
		return value
	}
	if len(args) < 2 {
		return seo
	}
	meta, _ := args[1].(map[string]string)
	if meta == nil {
		return seo
	}

	if t := strings.TrimSpace(meta[metaKeyTitle]); t != "" {
		seo.Title = t
		seo.OGTitle = t
	}
	if d := strings.TrimSpace(meta[metaKeyDescription]); d != "" {
		seo.Description = d
		seo.OGDescription = d
	}
	if img := strings.TrimSpace(meta[metaKeyImage]); img != "" {
		// Absolute URLs pass through; relative paths are resolved by the
		// theme's image helper / browser. Keeping it as-is matches how
		// SEOBuilder handles c.ImageURL.
		seo.OGImage = img
	}
	if r := strings.TrimSpace(meta[metaKeyRobots]); r != "" {
		seo.Robots = r
	}
	return seo
}

// --- HTML builder ---

// buildMetaBoxHTML produces a self-contained <details> panel with four form
// fields. Plain HTML keeps the plugin free of template dependencies and
// works with whatever admin styling lives in core.
func buildMetaBoxHTML(meta map[string]string) template.HTML {
	title := htmlAttr(meta[metaKeyTitle])
	description := htmlText(meta[metaKeyDescription])
	image := htmlAttr(meta[metaKeyImage])
	robots := meta[metaKeyRobots]

	return template.HTML(fmt.Sprintf(`
<details class="form-group" style="border:1px solid #e2e8f0; border-radius:8px; padding:0.75rem 1rem; margin-top:1rem;">
    <summary style="cursor:pointer; font-weight:600; padding:0.25rem 0;">SEO 设置（可选）</summary>
    <p style="margin:0.5rem 0 1rem; color:#64748b; font-size:0.85rem;">
        留空时使用默认值（标题用内容标题、描述用摘要、图片用主图）。仅在需要单独覆盖时填写。
    </p>

    <div class="form-group">
        <label for="_seo_title">SEO Title</label>
        <input type="text" id="_seo_title" name="_seo_title" value="%s" placeholder="留空则用内容标题" maxlength="120">
        <small style="color:#64748b;">推荐 50–60 字符。会覆盖 &lt;title&gt; 中"内容标题"那部分及 og:title。</small>
    </div>

    <div class="form-group">
        <label for="_seo_description">SEO Description</label>
        <textarea id="_seo_description" name="_seo_description" rows="3" placeholder="留空则用内容摘要 Excerpt" maxlength="320">%s</textarea>
        <small style="color:#64748b;">推荐 50–160 字符。会覆盖 &lt;meta description&gt; 和 og:description。</small>
    </div>

    <div class="form-group">
        <label for="_seo_image">Open Graph 分享图</label>
        <div style="display:flex; gap:0.5rem; align-items:flex-start;">
            <input type="text" id="_seo_image" name="_seo_image" value="%s" placeholder="留空则用内容主图">
            <button type="button" class="btn btn-secondary" style="white-space:nowrap;" onclick="if(window.openMediaPicker){openMediaPicker(function(url){document.getElementById('_seo_image').value=url;});}">选择图片</button>
        </div>
        <small style="color:#64748b;">仅作社交卡片用，不会改变内容详情页主图。</small>
    </div>

    <div class="form-group">
        <label for="_seo_robots">Robots 指令</label>
        <select id="_seo_robots" name="_seo_robots">
            <option value="" %s>默认（index, follow）</option>
            <option value="noindex" %s>noindex（不被索引，仍跟随链接）</option>
            <option value="noindex, nofollow" %s>noindex, nofollow（既不索引也不跟随）</option>
            <option value="nofollow" %s>nofollow（被索引但不跟随）</option>
        </select>
        <small style="color:#64748b;">用于已下架但保留页面、内部测试页、转载内容等场景。</small>
    </div>
</details>
`,
		title,
		description,
		image,
		selectedAttr(robots, ""),
		selectedAttr(robots, "noindex"),
		selectedAttr(robots, "noindex, nofollow"),
		selectedAttr(robots, "nofollow"),
	))
}

// --- helpers ---

// htmlAttr escapes a string for use inside an HTML attribute value.
func htmlAttr(s string) string {
	r := strings.NewReplacer(`&`, "&amp;", `"`, "&quot;", `<`, "&lt;", `>`, "&gt;")
	return r.Replace(s)
}

// htmlText escapes a string for use as HTML text content (textarea body).
func htmlText(s string) string {
	r := strings.NewReplacer(`&`, "&amp;", `<`, "&lt;", `>`, "&gt;")
	return r.Replace(s)
}

// selectedAttr returns ` selected` when current matches the option value,
// otherwise empty. Used to mark the active <option> in the robots select.
func selectedAttr(current, optionValue string) string {
	if current == optionValue {
		return "selected"
	}
	return ""
}
