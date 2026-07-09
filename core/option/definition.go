package option

// Definition describes a core option that is owned by GoPress itself.
//
// It is deliberately UI-framework agnostic: admin pages, plugins, and themes
// can consume the same metadata without importing each other.
type Definition struct {
	Key            string
	Section        string
	LabelKey       string
	Label          string
	DescriptionKey string
	InputType      string
	DefaultValue   string
	ReadOnly       bool
	Translatable   bool
}

const (
	SystemOptionSectionSite  = "site"
	SystemOptionSectionAdmin = "admin"

	SystemOptionInputText     = "text"
	SystemOptionInputSelect   = "select"
	SystemOptionInputMedia    = "media"
	SystemOptionInputCheckbox = "checkbox"
)

var systemOptionDefinitions = []Definition{
	{
		Key:            "active_theme",
		Section:        SystemOptionSectionSite,
		LabelKey:       "field.active_theme",
		Label:          "Active Theme",
		DescriptionKey: "help.active_theme",
		InputType:      SystemOptionInputText,
		ReadOnly:       true,
	},
	{
		Key:          "site_name",
		Section:      SystemOptionSectionSite,
		LabelKey:     "field.site_name",
		Label:        "Site Name",
		InputType:    SystemOptionInputText,
		Translatable: true,
	},
	{
		Key:          "site_description",
		Section:      SystemOptionSectionSite,
		LabelKey:     "field.site_description",
		Label:        "Site Description",
		InputType:    SystemOptionInputText,
		Translatable: true,
	},
	{
		Key:            "site_icon",
		Section:        SystemOptionSectionSite,
		LabelKey:       "field.site_icon",
		Label:          "Site Icon",
		DescriptionKey: "help.site_icon",
		InputType:      SystemOptionInputMedia,
	},
	{
		Key:            "site_language",
		Section:        SystemOptionSectionSite,
		LabelKey:       "field.site_language",
		Label:          "Site Language",
		DescriptionKey: "help.site_language",
		InputType:      SystemOptionInputSelect,
	},
	{
		Key:            "site_timezone",
		Section:        SystemOptionSectionSite,
		LabelKey:       "field.site_timezone",
		Label:          "Site Timezone",
		DescriptionKey: "help.site_timezone",
		InputType:      SystemOptionInputSelect,
	},
	{
		Key:            "powered_by_gopress",
		Section:        SystemOptionSectionSite,
		LabelKey:       "field.powered_by_gopress",
		Label:          "GoPress Attribution",
		DescriptionKey: "help.powered_by_gopress",
		InputType:      SystemOptionInputCheckbox,
		DefaultValue:   "1",
	},
	{
		Key:            "admin_language",
		Section:        SystemOptionSectionAdmin,
		LabelKey:       "field.admin_language",
		Label:          "Admin Interface Language",
		DescriptionKey: "help.admin_language",
		InputType:      SystemOptionInputSelect,
		DefaultValue:   "en",
	},
	{
		Key:       "admin_email",
		Section:   SystemOptionSectionAdmin,
		LabelKey:  "field.admin_email",
		Label:     "Admin Email",
		InputType: SystemOptionInputText,
	},
}

// AllSystemDefinitions returns all core-owned option definitions.
func AllSystemDefinitions() []Definition {
	out := make([]Definition, len(systemOptionDefinitions))
	copy(out, systemOptionDefinitions)
	return out
}

// SystemDefinition returns the core option definition for key, if any.
func SystemDefinition(key string) (Definition, bool) {
	for _, def := range systemOptionDefinitions {
		if def.Key == key {
			return def, true
		}
	}
	return Definition{}, false
}

// IsSystemSetting reports whether key is a declared core-owned option.
func IsSystemSetting(key string) bool {
	_, ok := SystemDefinition(key)
	return ok
}

// SystemDefaults returns default values for core options that define one.
func SystemDefaults() map[string]string {
	out := make(map[string]string, len(systemOptionDefinitions))
	for _, def := range systemOptionDefinitions {
		out[def.Key] = def.DefaultValue
	}
	return out
}

// SystemTranslatableDefinitions returns core options that can be translated.
func SystemTranslatableDefinitions() []Definition {
	out := make([]Definition, 0, len(systemOptionDefinitions))
	for _, def := range systemOptionDefinitions {
		if def.Translatable {
			out = append(out, def)
		}
	}
	return out
}

// IsSystemTranslatable checks if an option key is a translatable core option.
func IsSystemTranslatable(key string) bool {
	def, ok := SystemDefinition(key)
	return ok && def.Translatable
}
