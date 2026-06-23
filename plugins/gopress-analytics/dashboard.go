package gopressanalytics

import (
	"bytes"
	"context"
	"encoding/json"
	"html/template"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"go-press/core/i18n"
)

type widgetView struct {
	Title         string
	PVLabel       string
	UVLabel       string
	NewLabel      string
	SessionLabel  string
	TrendLabel    string
	TopLabel      string
	MixLabel      string
	ReturnLabel   string
	XAxisLabel    string
	YAxisLabel    string
	ShowMoreLabel string
	ShowLessLabel string
	PageLabel     string
	EmptyLabel    string
	DaysLabel     string
	InitialJSON   template.JS
}

func (p *Plugin) handleSummary(c *gin.Context) {
	if !p.active.Load() || p.summary == nil {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	days, err := strconv.Atoi(c.DefaultQuery("days", "30"))
	if err != nil || (days != 7 && days != 30 && days != 90) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "days must be one of 7, 30, or 90"})
		return
	}
	summary, err := p.summary.Summary(c.Request.Context(), days, p.location)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "analytics summary unavailable"})
		return
	}
	c.Header("Cache-Control", "private, no-store")
	c.JSON(http.StatusOK, summary)
}

func (p *Plugin) renderDashboardWidget(value interface{}, args ...interface{}) interface{} {
	existing := htmlValue(value)
	if !p.active.Load() || p.summary == nil || p.widget == nil || p.engine == nil || len(args) == 0 {
		return existing
	}
	root, ok := args[0].(map[string]interface{})
	if !ok {
		if ginRoot, ok := args[0].(gin.H); ok {
			root = ginRoot
		} else {
			return existing
		}
	}
	role, _ := root["CurrentRole"].(string)
	if !p.engine.RBAC.Can(role, "analytics", "read") {
		return existing
	}
	lang, _ := root["AdminLanguage"].(string)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	summary, err := p.summary.Summary(ctx, 30, p.location)
	if err != nil {
		return existing
	}
	payload, err := json.Marshal(summary)
	if err != nil {
		return existing
	}
	view := widgetView{
		Title:         analyticsText(lang, "analytics.title"),
		PVLabel:       analyticsText(lang, "analytics.pv"),
		UVLabel:       analyticsText(lang, "analytics.uv"),
		NewLabel:      analyticsText(lang, "analytics.new_visitors"),
		SessionLabel:  analyticsText(lang, "analytics.sessions"),
		TrendLabel:    analyticsText(lang, "analytics.trend"),
		TopLabel:      analyticsText(lang, "analytics.top_pages"),
		MixLabel:      analyticsText(lang, "analytics.visitor_mix"),
		ReturnLabel:   analyticsText(lang, "analytics.returning_visitors"),
		XAxisLabel:    analyticsText(lang, "analytics.x_axis"),
		YAxisLabel:    analyticsText(lang, "analytics.y_axis"),
		ShowMoreLabel: analyticsText(lang, "analytics.show_more"),
		ShowLessLabel: analyticsText(lang, "analytics.show_less"),
		PageLabel:     analyticsText(lang, "analytics.page"),
		EmptyLabel:    analyticsText(lang, "analytics.empty"),
		DaysLabel:     analyticsText(lang, "analytics.days"),
		InitialJSON:   template.JS(payload),
	}
	var out bytes.Buffer
	if err := p.widget.ExecuteTemplate(&out, "analytics-dashboard-widget", view); err != nil {
		return existing
	}
	return existing + template.HTML(out.String())
}

func htmlValue(value interface{}) template.HTML {
	switch v := value.(type) {
	case template.HTML:
		return v
	case string:
		return template.HTML(v)
	default:
		return ""
	}
}

var analyticsMessages = map[string]map[string]string{
	"en": {
		"analytics.title":              "Website Analytics",
		"analytics.pv":                 "Page views",
		"analytics.uv":                 "Unique visitors",
		"analytics.new_visitors":       "New visitors",
		"analytics.sessions":           "Sessions",
		"analytics.trend":              "Traffic trend",
		"analytics.top_pages":          "Top pages",
		"analytics.visitor_mix":        "Visitor mix",
		"analytics.returning_visitors": "Returning visitors",
		"analytics.x_axis":             "Date",
		"analytics.y_axis":             "Page views",
		"analytics.show_more":          "Show more",
		"analytics.show_less":          "Show less",
		"analytics.page":               "Page",
		"analytics.empty":              "No analytics data yet.",
		"analytics.days":               "days",
	},
	"zh-CN": {
		"analytics.title":              "网站访问统计",
		"analytics.pv":                 "浏览量 PV",
		"analytics.uv":                 "访客数 UV",
		"analytics.new_visitors":       "新访客",
		"analytics.sessions":           "会话数",
		"analytics.trend":              "访问趋势",
		"analytics.top_pages":          "热门页面",
		"analytics.visitor_mix":        "访客构成",
		"analytics.returning_visitors": "回访访客",
		"analytics.x_axis":             "日期",
		"analytics.y_axis":             "浏览量 PV",
		"analytics.show_more":          "展开更多",
		"analytics.show_less":          "收起",
		"analytics.page":               "页面",
		"analytics.empty":              "暂无访问统计数据。",
		"analytics.days":               "天",
	},
}

func analyticsText(lang, key string) string {
	lang = i18n.NormalizeSupportedLanguage(lang, []string{"en", "zh-CN"}, "zh-CN")
	if value := analyticsMessages[lang][key]; value != "" {
		return value
	}
	return analyticsMessages["en"][key]
}
