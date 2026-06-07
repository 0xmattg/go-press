package admin

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

const (
	defaultAdminListPerPage = 20
	maxAdminListPerPage     = 200
)

var defaultAdminListPerPageChoices = []int{20, 50, 100}

type adminListPrefs struct {
	Columns []string `json:"columns"`
	PerPage int      `json:"per_page"`
}

func adminListOptionName(key string) string {
	return "admin.list." + key
}

func (s *Service) LoadAdminListOptions(key string, columns []AdminListColumn, fallbackPerPage int) AdminListScreenOptions {
	if fallbackPerPage <= 0 {
		fallbackPerPage = defaultAdminListPerPage
	}
	prefs := adminListPrefs{PerPage: fallbackPerPage}
	if raw := strings.TrimSpace(s.options.Get(adminListOptionName(key))); raw != "" {
		_ = json.Unmarshal([]byte(raw), &prefs)
	}

	visible := sanitizeAdminListColumns(columns, prefs.Columns)
	return AdminListScreenOptions{
		Key:            key,
		Columns:        applyAdminListVisibility(columns, visible),
		PerPage:        normalizeAdminListPerPage(prefs.PerPage, fallbackPerPage),
		PerPageChoices: append([]int(nil), defaultAdminListPerPageChoices...),
	}
}

func (s *Service) SaveAdminListOptions(key string, columns []AdminListColumn, selectedColumns []string, perPageValue string, fallbackPerPage int) error {
	if fallbackPerPage <= 0 {
		fallbackPerPage = defaultAdminListPerPage
	}
	perPage, err := strconv.Atoi(strings.TrimSpace(perPageValue))
	if err != nil {
		perPage = fallbackPerPage
	}
	prefs := adminListPrefs{
		Columns: sanitizeAdminListColumns(columns, selectedColumns),
		PerPage: normalizeAdminListPerPage(perPage, fallbackPerPage),
	}
	data, err := json.Marshal(prefs)
	if err != nil {
		return err
	}
	if err := s.options.Set(adminListOptionName(key), string(data)); err != nil {
		return fmt.Errorf("save list options: %w", err)
	}
	return nil
}

func sanitizeAdminListColumns(columns []AdminListColumn, selected []string) []string {
	allowed := make(map[string]bool, len(columns))
	required := make(map[string]bool, len(columns))
	for _, col := range columns {
		allowed[col.Key] = true
		if col.Required {
			required[col.Key] = true
		}
	}

	seen := make(map[string]bool, len(selected))
	for _, key := range selected {
		if allowed[key] {
			seen[key] = true
		}
	}
	for key := range required {
		seen[key] = true
	}

	if len(seen) == 0 {
		for _, col := range columns {
			seen[col.Key] = true
		}
	}

	out := make([]string, 0, len(columns))
	for _, col := range columns {
		if seen[col.Key] {
			out = append(out, col.Key)
		}
	}
	return out
}

func applyAdminListVisibility(columns []AdminListColumn, visibleKeys []string) []AdminListColumn {
	visible := make(map[string]bool, len(visibleKeys))
	for _, key := range visibleKeys {
		visible[key] = true
	}
	out := make([]AdminListColumn, len(columns))
	for i, col := range columns {
		col.Visible = visible[col.Key] || col.Required
		out[i] = col
	}
	return out
}

func normalizeAdminListPerPage(perPage, fallback int) int {
	if fallback <= 0 {
		fallback = defaultAdminListPerPage
	}
	if perPage <= 0 {
		return fallback
	}
	if perPage > maxAdminListPerPage {
		return maxAdminListPerPage
	}
	return perPage
}

func adminVisibleColumnMap(columns []AdminListColumn) map[string]bool {
	visible := make(map[string]bool, len(columns))
	for _, col := range columns {
		if col.Visible || col.Required {
			visible[col.Key] = true
		}
	}
	return visible
}

func adminVisibleColumnCount(columns []AdminListColumn) int {
	count := 0
	for _, col := range columns {
		if col.Visible || col.Required {
			count++
		}
	}
	if count == 0 {
		return 1
	}
	return count
}
