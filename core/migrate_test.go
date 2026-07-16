package core

import (
	"reflect"
	"testing"

	"go-press/core/user"
)

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
