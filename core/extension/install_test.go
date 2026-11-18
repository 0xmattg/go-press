// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package extension

import (
	"testing"
	"testing/fstest"

	"github.com/0xmattg/go-press/pkg/sign"
)

// signedFS returns package files signed by k's publisher and countersigned
// by market (nil leaves the countersignature out).
func signedFS(t *testing.T, k testKeys, id sign.PackageID, market *sign.PrivateKey) fstest.MapFS {
	t.Helper()
	fsys := fstest.MapFS{
		"theme.toml":                   {Data: []byte("[theme]\nslug = \"aurora\"\n")},
		"templates/pages/home.tmpl":    {Data: []byte(`{{define "content"}}home{{end}}`)},
		"static/css/style.css":         {Data: []byte("body{}")},
		"locales/en.json":              {Data: []byte("{}")},
		"templates/layouts/base.tmpl":  {Data: []byte(`{{define "base"}}{{block "content" .}}{{end}}{{end}}`)},
		"templates/partials/head.tmpl": {Data: []byte(`{{define "head"}}{{end}}`)},
	}
	digest, err := sign.PackageDigest(fsys)
	if err != nil {
		t.Fatal(err)
	}
	pub, err := k.publisher.Sign(sign.PublisherStatement(id, digest))
	if err != nil {
		t.Fatal(err)
	}
	pub.PublicKey = k.publisher.Public().String()
	data, err := sign.MarshalSignature(pub)
	if err != nil {
		t.Fatal(err)
	}
	fsys[sign.PublisherSignatureFile] = &fstest.MapFile{Data: data}
	if market != nil {
		sig, err := market.Sign(sign.MarketStatement(id, k.publisher.Public().KID(), digest))
		if err != nil {
			t.Fatal(err)
		}
		if data, err = sign.MarshalSignature(sig); err != nil {
			t.Fatal(err)
		}
		fsys[sign.MarketSignatureFile] = &fstest.MapFile{Data: data}
	}
	return fsys
}

func TestCheckUploadSignature(t *testing.T) {
	k := newTestKeys(t)
	root := k.root(t, "https://market.example.com/api/market/v1")
	id := sign.PackageID{Kind: KindTheme, Slug: "aurora", Version: "1.0.0"}

	got := CheckUploadSignature(signedFS(t, k, id, &k.packages), id, root)
	if !got.Verified() || got.PublisherKID != k.publisher.Public().KID() {
		t.Fatalf("countersigned package = %+v", got)
	}

	other := newTestKeys(t)
	changed := signedFS(t, k, id, &k.packages)
	changed["static/css/style.css"] = &fstest.MapFile{Data: []byte("body{color:red}")}
	unsigned := signedFS(t, k, id, nil)
	delete(unsigned, sign.PublisherSignatureFile)
	cases := map[string]struct {
		fsys fstest.MapFS
		id   sign.PackageID
		root TrustRoot
		want string
	}{
		"no signature files":           {unsigned, id, root, SignatureMissing},
		"publisher signature only":     {signedFS(t, k, id, nil), id, root, SignatureMissing},
		"countersigned by another key": {signedFS(t, k, id, &other.packages), id, root, SignatureInvalid},
		"file changed after signing":   {changed, id, root, SignatureInvalid},
		"signed as another version":    {signedFS(t, k, id, &k.packages), sign.PackageID{Kind: KindTheme, Slug: "aurora", Version: "1.0.1"}, root, SignatureInvalid},
		"no market configured":         {signedFS(t, k, id, &k.packages), id, TrustRoot{}, SignatureNoMarket},
	}
	for name, tc := range cases {
		got := CheckUploadSignature(tc.fsys, tc.id, tc.root)
		if got.State != tc.want || got.Verified() || got.PublisherKID != "" {
			t.Errorf("%s: %+v, want state %s", name, got, tc.want)
		}
	}
}
