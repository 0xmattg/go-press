package postgresql

import (
	"errors"
	"testing"

	"go-press/config"
)

func TestBuildDSNDefaultsSchemaToPublic(t *testing.T) {
	t.Parallel()

	dsn := BuildDSN(config.PGConfig{
		User:     "postgres",
		Password: "secret",
		Hostname: "localhost",
		Port:     "5432",
		Database: "gopress",
	})

	want := "host=localhost port=5432 user=postgres password=secret dbname=gopress search_path=public sslmode=disable"
	if dsn != want {
		t.Fatalf("BuildDSN() = %q, want %q", dsn, want)
	}
}

func TestIsMissingDatabaseError(t *testing.T) {
	t.Parallel()

	if !isMissingDatabaseError(errors.New(`FATAL: database "gopress" does not exist (SQLSTATE 3D000)`)) {
		t.Fatal("expected missing database error to be detected")
	}
	if isMissingDatabaseError(errors.New("permission denied")) {
		t.Fatal("did not expect non-missing-database error to be detected")
	}
}
