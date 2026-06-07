package admin

import (
	"testing"
)

func TestSanitizeAdminListColumnsDropsUnknownKeys(t *testing.T) {
	columns := []AdminListColumn{
		{Key: "id", Label: "ID"},
		{Key: "title", Label: "Title"},
		{Key: "actions", Label: "Actions"},
	}

	got := sanitizeAdminListColumns(columns, []string{"title", "missing"})
	if len(got) != 1 || got[0] != "title" {
		t.Fatalf("visible columns = %#v, want only title", got)
	}
}

func TestNormalizeAdminListPerPageBoundsValue(t *testing.T) {
	if got := normalizeAdminListPerPage(0, 20); got != 20 {
		t.Fatalf("zero per-page = %d, want fallback", got)
	}
	if got := normalizeAdminListPerPage(500, 20); got != maxAdminListPerPage {
		t.Fatalf("large per-page = %d, want max", got)
	}
}
