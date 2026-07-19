package core

import (
	"errors"
	"reflect"
	"testing"

	"go-press/core/user"
)

type recordingColumnAlterer struct {
	fields []string
	errAt  string
}

func (m *recordingColumnAlterer) AlterColumn(value interface{}, field string) error {
	if _, ok := value.(*user.User); !ok {
		return errors.New("unexpected migration model")
	}
	m.fields = append(m.fields, field)
	if field == m.errAt {
		return errors.New("alter failed")
	}
	return nil
}

func TestCoreModelsIncludeExternalIdentityAndPublicSession(t *testing.T) {
	models := coreModels()
	want := map[reflect.Type]bool{
		reflect.TypeOf(&user.UserIdentity{}): false,
		reflect.TypeOf(&user.UserSession{}):  false,
	}
	for _, model := range models {
		if _, ok := want[reflect.TypeOf(model)]; ok {
			want[reflect.TypeOf(model)] = true
		}
	}
	for modelType, found := range want {
		if !found {
			t.Fatalf("core migration missing %v", modelType)
		}
	}
}

func TestLegacyUserMigrationRelaxesExternalAccountColumns(t *testing.T) {
	migrator := &recordingColumnAlterer{}
	if err := migrateLegacyUserColumns(migrator); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(migrator.fields, []string{"Email", "PasswordHash"}) {
		t.Fatalf("altered columns = %#v", migrator.fields)
	}

	migrator = &recordingColumnAlterer{errAt: "Email"}
	if err := migrateLegacyUserColumns(migrator); err == nil {
		t.Fatal("column migration error was ignored")
	}
}
