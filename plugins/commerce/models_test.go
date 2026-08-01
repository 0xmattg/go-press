package commerce

import (
	"sync"
	"testing"

	"gorm.io/gorm/schema"
)

func TestRefundRemoteIDIndexIsGatewayScopedAndPartial(t *testing.T) {
	parsed, err := schema.Parse(&Refund{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	for _, index := range parsed.ParseIndexes() {
		if index.Name != "idx_commerce_refund_gateway_remote" {
			continue
		}
		if index.Class != "UNIQUE" || index.Where != "gateway_refund_id <> ''" {
			t.Fatalf("refund remote index = class:%q where:%q", index.Class, index.Where)
		}
		if len(index.Fields) != 2 || index.Fields[0].Name != "Gateway" || index.Fields[1].Name != "GatewayRefundID" {
			t.Fatalf("refund remote index fields = %#v, want Gateway + GatewayRefundID", index.Fields)
		}
		return
	}
	t.Fatal("gateway-scoped refund remote id index was not parsed")
}
