package multilang

import (
	"strings"

	"github.com/0xmattg/go-press/core/option"
	"github.com/0xmattg/go-press/pkg/logger"

	"gorm.io/gorm"
)

// LEGACY(multilang-slug): until multilang 2.0.8 the plugin's slug was
// "multi-language", so existing sites store its activation flag as
// plugin_active_multi-language and its settings under plugin_multi-language_*.
// GIP-M0002 §3.3 requires the slug to equal the directory name, so the slug
// became "multilang" and this file renames those option keys once.
//
// It must run before engine.LoadPlugin: core reads plugin_active_<slug> there
// and treats a missing flag as "activate", which would switch the plugin back
// on for sites that had disabled it.
//
// It is safe to keep indefinitely: after the first run no legacy key is left,
// and each boot costs one indexed query that returns no rows. Remove only after
// 1.0, once no known site still runs multilang 2.0.7 or earlier, and say in the
// release notes that such sites must upgrade through a release that still has
// this file. Remove legacy_slug_test.go with it.
const (
	legacyActiveKey      = "plugin_active_multi-language"
	legacySettingsPrefix = "plugin_multi-language_"
	activeKey            = "plugin_active_" + PluginName
	settingsPrefix       = "plugin_" + PluginName + "_"
)

// migrateLegacySlugOptions renames the legacy option keys in the current
// site's options table, keeping each row's value and autoload flag, and then
// refreshes the option cache. It returns the number of legacy rows handled.
//
// Rows are matched with substr instead of LIKE because "_" is a LIKE wildcard,
// and read straight from the table because Options.All only holds autoload
// rows. When the new key already exists (an upgrade after a rollback), the new
// key wins and the legacy row is dropped.
func migrateLegacySlugOptions(db *gorm.DB, store *option.Store) (int, error) {
	if db == nil {
		return 0, nil
	}
	var rows []option.Option
	if err := db.Where("name = ? OR substr(name, 1, ?) = ?", legacyActiveKey, len(legacySettingsPrefix), legacySettingsPrefix).
		Find(&rows).Error; err != nil {
		return 0, err
	}
	var legacy []option.Option
	for _, row := range rows {
		if row.Name == legacyActiveKey || strings.HasPrefix(row.Name, legacySettingsPrefix) {
			legacy = append(legacy, row)
		}
	}
	if len(legacy) == 0 {
		return 0, nil
	}

	err := db.Transaction(func(tx *gorm.DB) error {
		for _, row := range legacy {
			newName := renamedLegacyKey(row.Name)
			var existing int64
			if err := tx.Model(&option.Option{}).Where("name = ?", newName).Count(&existing).Error; err != nil {
				return err
			}
			if existing > 0 {
				logger.Warn("multilang: legacy option dropped because the new key already exists", "legacy", row.Name, "key", newName)
				if err := tx.Delete(&option.Option{}, row.ID).Error; err != nil {
					return err
				}
				continue
			}
			if err := tx.Model(&option.Option{}).Where("id = ?", row.ID).Update("name", newName).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}

	if store != nil {
		// The database no longer has these keys, so Delete only clears the
		// cached copies; LoadAll then caches the renamed autoload rows.
		for _, row := range legacy {
			_ = store.Delete(row.Name)
		}
		store.LoadAll()
	}
	logger.Info("multilang: migrated legacy option keys", "count", len(legacy))
	return len(legacy), nil
}

func renamedLegacyKey(name string) string {
	if name == legacyActiveKey {
		return activeKey
	}
	return settingsPrefix + strings.TrimPrefix(name, legacySettingsPrefix)
}
