package dbprefix

import "testing"

func TestPluginAndThemeTable(t *testing.T) {
	Set("gp_site2_")
	t.Cleanup(func() { Set(DefaultPrefix) })
	if got := PluginTable("commerce_usdt", "invoices"); got != "gp_site2_plgn_commerce_usdt_invoices" {
		t.Fatalf("PluginTable = %q", got)
	}
	if got := ThemeTable("financial_news", "tickers"); got != "gp_site2_thm_financial_news_tickers" {
		t.Fatalf("ThemeTable = %q", got)
	}
}

func TestTableNamesRejectInvalidIdentifiers(t *testing.T) {
	cases := []struct {
		name string
		call func()
	}{
		{"hyphenated plugin slug", func() { PluginTable("multi-language", "translations") }},
		{"empty plugin slug", func() { PluginTable("", "translations") }},
		{"upper-case table", func() { PluginTable("multilang", "Translations") }},
		{"hyphenated theme slug", func() { ThemeTable("financial-news", "tickers") }},
		{"injection", func() { ThemeTable("news", "tickers; drop table x") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("expected a panic")
				}
			}()
			tc.call()
		})
	}
}
