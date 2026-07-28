package contracts_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"

	"go-press/core/hook"
)

var standardFrontendHooks = []string{
	hook.ThemeHeadEnd,
	hook.ThemeBodyOpen,
	hook.ThemeFooterEnd,
	hook.ThemeHeaderNavAfter,
}

type hookOccurrence struct {
	path   string
	source string
	offset int
}

func TestBuiltInThemesExposeStandardFrontendHooks(t *testing.T) {
	repositoryRoot := testRepositoryRoot(t)
	manifests, err := filepath.Glob(filepath.Join(repositoryRoot, "themes", "*", "theme.toml"))
	if err != nil {
		t.Fatalf("discover theme manifests: %v", err)
	}
	if len(manifests) == 0 {
		t.Fatal("no built-in themes discovered")
	}

	for _, manifest := range manifests {
		themeDir := filepath.Dir(manifest)
		t.Run(filepath.Base(themeDir), func(t *testing.T) {
			templates := readThemeTemplates(t, themeDir)
			for _, hookName := range standardFrontendHooks {
				occurrences := findHookOccurrences(templates, hookName)
				if len(occurrences) != 1 {
					t.Fatalf("standard hook %q must be declared exactly once; found %d", hookName, len(occurrences))
				}

				occurrence := occurrences[0]
				if !usesCurrentTemplateData(occurrence.source, occurrence.offset, hookName) {
					t.Errorf("%s: standard hook %q must receive the current template data with {{renderHook %q .}}", occurrence.path, hookName, hookName)
				}

				switch hookName {
				case hook.ThemeHeadEnd:
					if !insideHTMLElement(occurrence.source, occurrence.offset, "head") {
						t.Errorf("%s: %q must be declared inside <head>", occurrence.path, hookName)
					}
					if !immediatelyBeforeClosingElement(occurrence.source, occurrence.offset, "head") {
						t.Errorf("%s: %q must be declared immediately before </head>", occurrence.path, hookName)
					}
				case hook.ThemeBodyOpen:
					if !insideHTMLElement(occurrence.source, occurrence.offset, "body") {
						t.Errorf("%s: %q must be declared inside <body>", occurrence.path, hookName)
					}
					if !immediatelyAfterOpeningElement(occurrence.source, occurrence.offset, "body") {
						t.Errorf("%s: %q must be declared immediately after <body>", occurrence.path, hookName)
					}
				case hook.ThemeFooterEnd:
					if !insideHTMLElement(occurrence.source, occurrence.offset, "body") {
						t.Errorf("%s: %q must be declared inside <body>", occurrence.path, hookName)
					}
					if !afterThemeScript(occurrence.source, occurrence.offset) {
						t.Errorf("%s: %q must be declared after the theme script", occurrence.path, hookName)
					}
					if !immediatelyBeforeClosingElement(occurrence.source, occurrence.offset, "body") {
						t.Errorf("%s: %q must be declared immediately before </body>", occurrence.path, hookName)
					}
				case hook.ThemeHeaderNavAfter:
					if !insideHTMLElement(occurrence.source, occurrence.offset, "ul") {
						t.Errorf("%s: %q must be declared inside a <ul> because navigation extensions render <li> items", occurrence.path, hookName)
					}
				}
			}
		})
	}
}

func testRepositoryRoot(t *testing.T) string {
	t.Helper()
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate theme contract test source")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "..", ".."))
}

func readThemeTemplates(t *testing.T, themeDir string) map[string]string {
	t.Helper()

	templates := make(map[string]string)
	templateDir := filepath.Join(themeDir, "templates")
	err := filepath.WalkDir(templateDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(path) != ".tmpl" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		templates[path] = string(data)
		return nil
	})
	if err != nil {
		t.Fatalf("read templates for %s: %v", themeDir, err)
	}
	if len(templates) == 0 {
		t.Fatalf("theme %s has no templates", themeDir)
	}
	return templates
}

func findHookOccurrences(templates map[string]string, hookName string) []hookOccurrence {
	pattern := regexp.MustCompile(`renderHook\s+"` + regexp.QuoteMeta(hookName) + `"`)
	paths := make([]string, 0, len(templates))
	for path := range templates {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	var occurrences []hookOccurrence
	for _, path := range paths {
		source := templates[path]
		for _, match := range pattern.FindAllStringIndex(source, -1) {
			occurrences = append(occurrences, hookOccurrence{path: path, source: source, offset: match[0]})
		}
	}
	return occurrences
}

func usesCurrentTemplateData(source string, offset int, hookName string) bool {
	end := strings.Index(source[offset:], "}}")
	if end < 0 {
		return false
	}
	action := source[offset : offset+end+2]
	pattern := regexp.MustCompile(fmt.Sprintf(`^renderHook\s+%q\s+\.\s*-?}}$`, hookName))
	return pattern.MatchString(strings.TrimSpace(action))
}

func insideHTMLElement(source string, offset int, tagName string) bool {
	tagPattern := regexp.MustCompile(`(?i)</?` + regexp.QuoteMeta(tagName) + `\b[^>]*>`)
	depth := 0
	for _, match := range tagPattern.FindAllString(source[:offset], -1) {
		if strings.HasPrefix(strings.ToLower(match), "</") {
			depth--
			continue
		}
		depth++
	}
	return depth > 0
}

func afterThemeScript(source string, offset int) bool {
	return strings.LastIndex(strings.ToLower(source[:offset]), "<script") >= 0
}

func immediatelyAfterOpeningElement(source string, offset int, tagName string) bool {
	tagPattern := regexp.MustCompile(`(?i)<` + regexp.QuoteMeta(tagName) + `\b[^>]*>`)
	tags := tagPattern.FindAllStringIndex(source[:offset], -1)
	if len(tags) == 0 {
		return false
	}
	actionStart := strings.LastIndex(source[:offset], "{{")
	if actionStart < 0 {
		return false
	}
	return strings.TrimSpace(source[tags[len(tags)-1][1]:actionStart]) == ""
}

func immediatelyBeforeClosingElement(source string, offset int, tagName string) bool {
	actionEnd := strings.Index(source[offset:], "}}")
	if actionEnd < 0 {
		return false
	}
	actionEnd += offset + 2
	closingPattern := regexp.MustCompile(`(?i)</` + regexp.QuoteMeta(tagName) + `\s*>`)
	closing := closingPattern.FindStringIndex(source[actionEnd:])
	if closing == nil {
		return false
	}
	return strings.TrimSpace(source[actionEnd:actionEnd+closing[0]]) == ""
}
