package core

import (
	"testing"

	"go-press/pkg/dbprefix"
)

func TestForceSeedClearTablesPreservesUsers(t *testing.T) {
	prev := dbprefix.Get()
	dbprefix.Set("test_")
	defer dbprefix.Set(prev)

	forbidden := map[string]bool{
		dbprefix.Table("users"):     true,
		dbprefix.Table("user_meta"): true,
	}
	for _, table := range forceSeedClearTables() {
		if forbidden[table] {
			t.Fatalf("force seed must not clear user table %q", table)
		}
	}
}
