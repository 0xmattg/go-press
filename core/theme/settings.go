// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

// Declared theme settings.
//
// A theme lists its settings in theme.toml as [[settings]] entries, grouped by
// optional [[setting_groups]]. Core then owns everything generic about them:
// it validates and saves only declared keys, registers the translatable ones
// when the theme activates, fills unsaved keys with their defaults in the
// template data, and builds the admin form when the theme has no settings
// template of its own. Themes without code, such as installed packages, rely
// on this entirely.
//
// Settings are site-wide options: a key means the same option whichever theme
// declares it, so values a theme shares with another (company details, social
// links) carry over when switching themes. Keys owned by plugins, by the
// extension market, or by core's own behavior cannot be declared, so a theme
// settings form can never change them.

package theme

import (
	"errors"
	"fmt"
	"math"
	"net/mail"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/0xmattg/go-press/core/option"
)

// Setting types. string, text, int, bool, select and image match the types of
// content meta fields; url and email are specific to settings.
const (
	SettingString = "string" // single line
	SettingText   = "text"   // multiple lines
	SettingInt    = "int"
	SettingBool   = "bool" // stored as "1" or "0"
	SettingSelect = "select"
	SettingImage  = "image" // a site path or http(s) URL, picked from the media library
	SettingURL    = "url"
	SettingEmail  = "email"
)

const (
	// MaxSettingFields bounds the declared settings after repeat expansion.
	MaxSettingFields = 500
	// MaxSettingRepeat bounds the repeat count of one entry.
	MaxSettingRepeat = 24

	maxSettingKeyLength  = 64
	defaultLineMaxLength = 2048
	defaultTextMaxLength = 20000
	maxSettingMaxLength  = 100000
	repeatPlaceholder    = "{n}"
)

// reservedSettingPrefixes are option namespaces a theme may not declare:
// plugin settings (saved under the plugin's own permission), the extension
// market, and core's demo import markers.
var reservedSettingPrefixes = []string{"plugin_", "market_", "demo_imported_"}

var settingKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// SettingGroupConfig maps a [[setting_groups]] entry. Groups order the
// sections of the generated settings form.
type SettingGroupConfig struct {
	Key      string `toml:"key"`
	Label    string `toml:"label"`
	LabelKey string `toml:"label_key"`
}

// SettingOptionConfig is one choice of a select setting.
type SettingOptionConfig struct {
	Value    string `toml:"value"`
	Label    string `toml:"label"`
	LabelKey string `toml:"label_key"`
}

// SettingDefault is a setting's default value. theme.toml may write it as a
// string, a number or a boolean; it is kept as the string the option stores.
type SettingDefault string

// UnmarshalTOML accepts strings, integers, floats and booleans.
func (d *SettingDefault) UnmarshalTOML(v interface{}) error {
	switch value := v.(type) {
	case string:
		*d = SettingDefault(value)
	case int64:
		*d = SettingDefault(strconv.FormatInt(value, 10))
	case float64:
		*d = SettingDefault(strconv.FormatFloat(value, 'f', -1, 64))
	case bool:
		if value {
			*d = "1"
		} else {
			*d = "0"
		}
	default:
		return fmt.Errorf("unsupported default %v", v)
	}
	return nil
}

// SettingConfig maps a [[settings]] entry. With Repeat N (2–24), the entry
// stands for N settings: "{n}" in Key, Label, Help and their translations is
// replaced by 1…N.
type SettingConfig struct {
	Key          string                `toml:"key"`
	Group        string                `toml:"group"`
	Type         string                `toml:"type"`
	Label        string                `toml:"label"`
	LabelKey     string                `toml:"label_key"`
	Help         string                `toml:"help"`
	HelpKey      string                `toml:"help_key"`
	Default      SettingDefault        `toml:"default"`
	Translatable bool                  `toml:"translatable"`
	Required     bool                  `toml:"required"`
	MaxLength    int                   `toml:"max_length"`
	Min          *int                  `toml:"min"`
	Max          *int                  `toml:"max"`
	Repeat       int                   `toml:"repeat"`
	Options      []SettingOptionConfig `toml:"options"`
}

// SettingField is one declared setting after repeat expansion.
type SettingField struct {
	Key          string
	Group        string
	Type         string
	Label        string // fallback text, "{n}" already replaced
	LabelKey     string // theme admin locale key; "{n}" in its message is replaced by N
	Help         string
	HelpKey      string
	N            int // 1…repeat for repeated entries, 0 otherwise
	Default      string
	Translatable bool
	Required     bool
	MaxLength    int // characters; 0 for int, bool and select
	Min          *int
	Max          *int
	Options      []SettingOptionConfig
}

// SettingsSchema is a validated settings declaration.
type SettingsSchema struct {
	Groups []SettingGroupConfig
	Fields []SettingField
	index  map[string]int
}

// Field returns the declared setting with key.
func (s *SettingsSchema) Field(key string) (SettingField, bool) {
	if s == nil {
		return SettingField{}, false
	}
	i, ok := s.index[key]
	if !ok {
		return SettingField{}, false
	}
	return s.Fields[i], true
}

// ApplyDefaults sets the default of every declared key that settings does not
// contain and returns settings. A key that is present keeps its value, even
// when empty: an empty saved value means the setting was cleared.
func (s *SettingsSchema) ApplyDefaults(settings map[string]string) map[string]string {
	if s == nil {
		return settings
	}
	if settings == nil {
		settings = make(map[string]string, len(s.Fields))
	}
	for _, field := range s.Fields {
		if _, saved := settings[field.Key]; !saved && field.Default != "" {
			settings[field.Key] = field.Default
		}
	}
	return settings
}

// RegisterTranslatableSettings registers the schema's translatable settings
// with option.RegisterTranslatable, using the group as section and the
// fallback label. Engine calls it while a theme activation is staged.
func RegisterTranslatableSettings(s *SettingsSchema) {
	if s == nil {
		return
	}
	for _, field := range s.Fields {
		if field.Translatable {
			option.RegisterTranslatable(field.Key, field.Group, field.Label)
		}
	}
}

// SettingsSchema validates the theme's [[setting_groups]] and [[settings]]
// and returns them expanded. It returns nil and no error when the theme
// declares no settings. The result is computed once per FileConfig.
func (c *FileConfig) SettingsSchema() (*SettingsSchema, error) {
	if c == nil {
		return nil, nil
	}
	c.schemaOnce.Do(func() {
		c.schema, c.schemaErr = buildSettingsSchema(c.SettingGroups, c.Settings)
	})
	return c.schema, c.schemaErr
}

// ValidateSettings reports whether the settings declaration is valid.
func (c *FileConfig) ValidateSettings() error {
	_, err := c.SettingsSchema()
	return err
}

func buildSettingsSchema(groups []SettingGroupConfig, entries []SettingConfig) (*SettingsSchema, error) {
	if len(entries) == 0 {
		if len(groups) > 0 {
			return nil, errors.New("[[setting_groups]] declared without [[settings]]")
		}
		return nil, nil
	}
	schema := &SettingsSchema{index: map[string]int{}}
	groupKeys := map[string]bool{}
	for i, group := range groups {
		group.Key = strings.TrimSpace(group.Key)
		if !validSettingName(group.Key) {
			return nil, fmt.Errorf("setting group %d: key %q must be lowercase letters, digits and underscores", i+1, group.Key)
		}
		if groupKeys[group.Key] {
			return nil, fmt.Errorf("setting group %q declared twice", group.Key)
		}
		groupKeys[group.Key] = true
		schema.Groups = append(schema.Groups, group)
	}
	for i, entry := range entries {
		fields, err := expandSetting(entry, groupKeys)
		if err != nil {
			name := strings.TrimSpace(entry.Key)
			if name == "" {
				name = fmt.Sprintf("#%d", i+1)
			}
			return nil, fmt.Errorf("setting %s: %w", name, err)
		}
		for _, field := range fields {
			if _, dup := schema.index[field.Key]; dup {
				return nil, fmt.Errorf("setting %s declared twice", field.Key)
			}
			if len(schema.Fields) == MaxSettingFields {
				return nil, fmt.Errorf("more than %d settings declared", MaxSettingFields)
			}
			schema.index[field.Key] = len(schema.Fields)
			schema.Fields = append(schema.Fields, field)
		}
	}
	return schema, nil
}

func expandSetting(entry SettingConfig, groups map[string]bool) ([]SettingField, error) {
	key := strings.TrimSpace(entry.Key)
	repeated := strings.Contains(key, repeatPlaceholder)
	count := 1
	switch {
	case entry.Repeat == 0 || entry.Repeat == 1:
		if repeated {
			return nil, errors.New(`"{n}" in the key needs repeat between 2 and 24`)
		}
	case entry.Repeat >= 2 && entry.Repeat <= MaxSettingRepeat:
		if !repeated {
			return nil, errors.New(`repeat needs "{n}" in the key`)
		}
		count = entry.Repeat
	default:
		return nil, fmt.Errorf("repeat must be between 2 and %d", MaxSettingRepeat)
	}

	base := SettingField{
		Group:        strings.TrimSpace(entry.Group),
		Type:         strings.TrimSpace(entry.Type),
		Default:      string(entry.Default),
		Translatable: entry.Translatable,
		Required:     entry.Required,
		Min:          entry.Min,
		Max:          entry.Max,
		Options:      entry.Options,
		LabelKey:     strings.TrimSpace(entry.LabelKey),
		HelpKey:      strings.TrimSpace(entry.HelpKey),
	}
	if base.Group != "" && !groups[base.Group] {
		return nil, fmt.Errorf("group %q is not declared in [[setting_groups]]", base.Group)
	}
	if err := checkSettingType(&base, entry.MaxLength); err != nil {
		return nil, err
	}
	if base.Default != "" {
		if _, err := base.Validate(base.Default); err != nil {
			return nil, fmt.Errorf("default %q: %w", base.Default, err)
		}
	}

	fields := make([]SettingField, 0, count)
	for n := 1; n <= count; n++ {
		field := base
		field.Key, field.Label, field.Help = key, entry.Label, entry.Help
		if repeated {
			field.N = n
			number := strconv.Itoa(n)
			field.Key = strings.ReplaceAll(key, repeatPlaceholder, number)
			field.Label = strings.ReplaceAll(entry.Label, repeatPlaceholder, number)
			field.Help = strings.ReplaceAll(entry.Help, repeatPlaceholder, number)
		}
		field.Label = strings.TrimSpace(field.Label)
		if field.Label == "" {
			field.Label = field.Key
		}
		if !validSettingName(field.Key) {
			return nil, fmt.Errorf("key %q must start with a letter and use lowercase letters, digits and underscores, at most %d characters", field.Key, maxSettingKeyLength)
		}
		if reason := reservedSettingKey(field.Key); reason != "" {
			return nil, fmt.Errorf("key %q is reserved: %s", field.Key, reason)
		}
		fields = append(fields, field)
	}
	return fields, nil
}

// checkSettingType validates the type-specific attributes and fills the
// effective maximum length.
func checkSettingType(field *SettingField, maxLength int) error {
	if maxLength < 0 || maxLength > maxSettingMaxLength {
		return fmt.Errorf("max_length must be between 0 and %d", maxSettingMaxLength)
	}
	lengthAllowed, rangeAllowed, optionsAllowed, translatableAllowed := false, false, false, false
	switch field.Type {
	case SettingString, SettingURL, SettingImage, SettingEmail:
		lengthAllowed, translatableAllowed = true, true
		field.MaxLength = defaultLineMaxLength
	case SettingText:
		lengthAllowed, translatableAllowed = true, true
		field.MaxLength = defaultTextMaxLength
	case SettingInt:
		rangeAllowed = true
	case SettingBool:
	case SettingSelect:
		optionsAllowed = true
	case "":
		return errors.New("type is required")
	default:
		return fmt.Errorf("unknown type %q", field.Type)
	}
	if maxLength > 0 {
		if !lengthAllowed {
			return fmt.Errorf("max_length does not apply to %s settings", field.Type)
		}
		field.MaxLength = maxLength
	}
	if (field.Min != nil || field.Max != nil) && !rangeAllowed {
		return fmt.Errorf("min and max apply to int settings only")
	}
	if field.Min != nil && field.Max != nil && *field.Min > *field.Max {
		return errors.New("min is greater than max")
	}
	if field.Translatable && !translatableAllowed {
		return fmt.Errorf("%s settings cannot be translatable", field.Type)
	}
	if !optionsAllowed {
		if len(field.Options) > 0 {
			return fmt.Errorf("options apply to select settings only")
		}
		return nil
	}
	if len(field.Options) == 0 {
		return errors.New("select settings need options")
	}
	seen := map[string]bool{}
	for _, opt := range field.Options {
		if opt.Value == "" || seen[opt.Value] {
			return fmt.Errorf("option value %q is empty or repeated", opt.Value)
		}
		seen[opt.Value] = true
	}
	return nil
}

func validSettingName(name string) bool {
	return len(name) <= maxSettingKeyLength && settingKeyPattern.MatchString(name)
}

// reservedSettingKey explains why key cannot be a theme setting, or returns
// "". Core options can only be declared when they are translatable display
// text (the site name and description); behavioral options such as the
// active theme or registration rules stay on the system settings page.
func reservedSettingKey(key string) string {
	for _, prefix := range reservedSettingPrefixes {
		if strings.HasPrefix(key, prefix) {
			return "the " + prefix + " namespace is not owned by themes"
		}
	}
	if def, ok := option.SystemDefinition(key); ok && !def.Translatable {
		return "it is a core setting"
	}
	return ""
}

// Setting value problems, returned by SettingField.Validate as the Reason of
// a SettingValueError. The admin maps them to localized messages.
const (
	SettingErrRequired   = "required"
	SettingErrTooLong    = "too_long"
	SettingErrNotInt     = "not_int"
	SettingErrOutOfRange = "out_of_range"
	SettingErrNotBool    = "not_bool"
	SettingErrNotOption  = "not_option"
	SettingErrBadURL     = "bad_url"
	SettingErrBadImage   = "bad_image"
	SettingErrBadEmail   = "bad_email"
)

// SettingValueError reports why a value does not fit its setting.
type SettingValueError struct {
	Key    string
	Reason string
}

func (e *SettingValueError) Error() string {
	return fmt.Sprintf("setting %s: %s", e.Key, e.Reason)
}

// Validate checks value against the field and returns the value to store:
// int, bool, select, url, image and email values are trimmed, and bool
// values become "1" or "0". An empty value is allowed unless the field is
// required.
func (f SettingField) Validate(value string) (string, error) {
	fail := func(reason string) (string, error) {
		return "", &SettingValueError{Key: f.Key, Reason: reason}
	}
	switch f.Type {
	case SettingInt, SettingBool, SettingSelect, SettingURL, SettingImage, SettingEmail:
		value = strings.TrimSpace(value)
	}
	if value == "" {
		if f.Required {
			return fail(SettingErrRequired)
		}
		if f.Type == SettingBool {
			return "0", nil
		}
		return "", nil
	}
	if f.MaxLength > 0 && utf8.RuneCountInString(value) > f.MaxLength {
		return fail(SettingErrTooLong)
	}
	switch f.Type {
	case SettingInt:
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil || n > math.MaxInt32 || n < math.MinInt32 {
			return fail(SettingErrNotInt)
		}
		if (f.Min != nil && n < int64(*f.Min)) || (f.Max != nil && n > int64(*f.Max)) {
			return fail(SettingErrOutOfRange)
		}
		return strconv.FormatInt(n, 10), nil
	case SettingBool:
		switch strings.ToLower(value) {
		case "1", "true", "on":
			return "1", nil
		case "0", "false", "off":
			return "0", nil
		}
		return fail(SettingErrNotBool)
	case SettingSelect:
		for _, opt := range f.Options {
			if opt.Value == value {
				return value, nil
			}
		}
		return fail(SettingErrNotOption)
	case SettingURL:
		if !safeSettingURL(value, true) {
			return fail(SettingErrBadURL)
		}
	case SettingImage:
		if !safeSettingURL(value, false) {
			return fail(SettingErrBadImage)
		}
	case SettingEmail:
		addr, err := mail.ParseAddress(value)
		if err != nil || addr.Address != value {
			return fail(SettingErrBadEmail)
		}
	}
	return value, nil
}

// safeSettingURL accepts a site path ("/about", but not "//host"), an
// http(s) URL with a host, and for links also "#fragment", "mailto:" and
// "tel:". Anything else, such as "javascript:" or "data:", is refused, as is
// any whitespace or control character.
func safeSettingURL(value string, link bool) bool {
	for _, r := range value {
		if r <= ' ' || r == 0x7f || r == '\\' {
			return false
		}
	}
	if strings.HasPrefix(value, "/") {
		return !strings.HasPrefix(value, "//")
	}
	if link && strings.HasPrefix(value, "#") {
		return true
	}
	u, err := url.Parse(value)
	if err != nil {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		return u.Host != ""
	case "mailto", "tel":
		return link && u.Opaque != ""
	}
	return false
}
