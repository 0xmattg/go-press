package i18n

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"
)

func writeThemeLocale(t *testing.T, lang, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "locales"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "locales", lang+".json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func localize(m *Manager, lang, id string) string {
	msg, err := m.NewLocalizer(lang).Localize(&goi18n.LocalizeConfig{MessageID: id})
	if err != nil {
		return id
	}
	return msg
}

func TestThemeLocalesReplacePreviousTheme(t *testing.T) {
	m := NewManager("en")
	m.LoadThemeLocales(writeThemeLocale(t, "en", `{"hero.title":"Theme A","shared":"A"}`))
	if got := localize(m, "en", "hero.title"); got != "Theme A" {
		t.Fatalf("hero.title = %q, want Theme A", got)
	}

	m.LoadThemeLocales(writeThemeLocale(t, "en", `{"shared":"B"}`))
	if got := localize(m, "en", "hero.title"); got != "hero.title" {
		t.Fatalf("hero.title after switch = %q, want fallback to message ID", got)
	}
	if got := localize(m, "en", "shared"); got != "B" {
		t.Fatalf("shared after switch = %q, want B", got)
	}

	m.LoadThemeLocales(filepath.Join(t.TempDir(), "no-locales"))
	if got := localize(m, "en", "shared"); got != "shared" {
		t.Fatalf("shared after switching to a theme without locales = %q, want message ID", got)
	}
}

func TestOverridesSurviveThemeReload(t *testing.T) {
	m := NewManager("en")
	dir := writeThemeLocale(t, "zh", `{"nav.home":"首页"}`)
	m.LoadThemeLocales(dir)
	m.AddMessages("zh", []*goi18n.Message{{ID: "nav.home", Other: "主页"}})
	m.LoadThemeLocales(dir)
	if got := localize(m, "zh", "nav.home"); got != "主页" {
		t.Fatalf("nav.home = %q, want database override 主页", got)
	}
}

func TestExistingLocalizerKeepsItsSnapshot(t *testing.T) {
	m := NewManager("en")
	m.AddMessages("en", []*goi18n.Message{{ID: "greeting", Other: "Hello"}})
	pinned := m.NewLocalizer("en")
	m.AddMessages("en", []*goi18n.Message{{ID: "greeting", Other: "Hi"}})

	msg, err := pinned.Localize(&goi18n.LocalizeConfig{MessageID: "greeting"})
	if err != nil || msg != "Hello" {
		t.Fatalf("pinned localizer = %q, %v; want Hello from its original snapshot", msg, err)
	}
	if got := localize(m, "en", "greeting"); got != "Hi" {
		t.Fatalf("new localizer = %q, want Hi", got)
	}
}

func TestZeroValueManagerTranslates(t *testing.T) {
	var m Manager
	if got := m.Translate(nil, "missing.key"); got != "missing.key" {
		t.Fatalf("Translate() = %q, want message ID", got)
	}
}

// TestConcurrentUpdatesAndLocalization reproduces the audit's bundle race:
// translation saves and theme switches while requests localize. Run with -race.
func TestConcurrentUpdatesAndLocalization(t *testing.T) {
	m := NewManager("en")
	dirA := writeThemeLocale(t, "en", `{"title":"A"}`)
	dirB := writeThemeLocale(t, "en", `{"title":"B"}`)

	var wg sync.WaitGroup
	stop := make(chan struct{})
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_ = m.Translate(nil, "title")
					_ = localize(m, "zh", "title")
				}
			}
		}()
	}
	for i := 0; i < 100; i++ {
		if i%2 == 0 {
			m.LoadThemeLocales(dirA)
		} else {
			m.LoadThemeLocales(dirB)
		}
		m.AddMessages("zh", []*goi18n.Message{{ID: "title", Other: "标题"}})
	}
	close(stop)
	wg.Wait()
}
