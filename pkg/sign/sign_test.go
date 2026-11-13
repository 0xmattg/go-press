package sign

import (
	"archive/zip"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func mustKey(t *testing.T) PrivateKey {
	t.Helper()
	key, err := GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func TestKeysRoundTripAndKID(t *testing.T) {
	key := mustKey(t)
	pemData, err := key.MarshalPEM()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(pemData, []byte("-----BEGIN PRIVATE KEY-----")) {
		t.Fatalf("private key is not a PKCS#8 PEM block:\n%s", pemData)
	}
	parsed, err := ParsePrivateKeyPEM(pemData)
	if err != nil {
		t.Fatal(err)
	}
	if !parsed.Public().Equal(key.Public()) {
		t.Fatal("PEM round trip changed the key")
	}

	pub, err := ParsePublicKey(" " + key.Public().String() + "\n")
	if err != nil {
		t.Fatal(err)
	}
	if !pub.Equal(key.Public()) || pub.KID() != key.Public().KID() {
		t.Fatal("public key round trip changed the key or its ID")
	}
	if kid := pub.KID(); len(kid) != KIDLength || strings.Trim(kid, "0123456789abcdef") != "" {
		t.Fatalf("KID %q is not %d lowercase hex characters", kid, KIDLength)
	}
	if mustKey(t).Public().KID() == pub.KID() {
		t.Fatal("two keys share an ID")
	}

	for _, bad := range []string{"", "not base64!", "AAAA"} {
		if _, err := ParsePublicKey(bad); err == nil {
			t.Errorf("ParsePublicKey(%q) accepted a bad key", bad)
		}
	}
	if _, err := ParsePrivateKeyPEM([]byte("garbage")); err == nil {
		t.Error("ParsePrivateKeyPEM accepted garbage")
	}
}

func TestStatementEncodingIsExactAndStrict(t *testing.T) {
	id := PackageID{Kind: "theme", Slug: "aurora", Version: "1.2.0"}
	got, err := MarketStatement(id, "0011223344556677", "h1:abc=").Bytes()
	if err != nil {
		t.Fatal(err)
	}
	want := "gopress-sig-v1\npackage-market\ntheme\naurora\n1.2.0\n0011223344556677\nh1:abc=\n"
	if string(got) != want {
		t.Fatalf("statement bytes =\n%q\nwant\n%q", got, want)
	}
	if got, _ := CatalogStatement(42, "ff").Bytes(); string(got) != "gopress-sig-v1\ncatalog\n42\nff\n" {
		t.Fatalf("catalog statement = %q", got)
	}

	bad := []Statement{
		{Purpose: "Package", Fields: []string{"x"}},
		{Purpose: "", Fields: []string{"x"}},
		{Purpose: "catalog", Fields: []string{"1\n2", "ff"}},
		{Purpose: "catalog", Fields: []string{"", "ff"}},
		{Purpose: "catalog", Fields: []string{"1\r", "ff"}},
	}
	for _, statement := range bad {
		if _, err := statement.Bytes(); err == nil {
			t.Errorf("Bytes accepted %#v", statement)
		}
	}
}

func TestPackageIDValidation(t *testing.T) {
	good := PackageID{Kind: "theme", Slug: "modern-company", Version: "1.0.11"}
	if err := good.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, id := range []PackageID{
		{Kind: "Theme", Slug: "a1", Version: "1"},
		{Kind: "theme", Slug: "a", Version: "1"},
		{Kind: "theme", Slug: "bad_slug", Version: "1"},
		{Kind: "theme", Slug: "-bad", Version: "1"},
		{Kind: "theme", Slug: "double--hyphen", Version: "1"},
		{Kind: "theme", Slug: "ok-slug", Version: ""},
		{Kind: "theme", Slug: "ok-slug", Version: "1 2"},
	} {
		if err := id.Validate(); err == nil {
			t.Errorf("Validate accepted %#v", id)
		}
	}
}

func TestSignaturesAreBoundToKeyStatementAndPurpose(t *testing.T) {
	key, other := mustKey(t), mustKey(t)
	id := PackageID{Kind: "theme", Slug: "aurora", Version: "1.2.0"}
	statement := PublisherStatement(id, "h1:abc=")
	sig, err := key.Sign(statement)
	if err != nil {
		t.Fatal(err)
	}
	if sig.KID != key.Public().KID() || sig.Alg != Algorithm {
		t.Fatalf("signature metadata = %+v", sig)
	}
	if err := Verify(key.Public(), statement, sig); err != nil {
		t.Fatalf("valid signature rejected: %v", err)
	}

	changed := PublisherStatement(PackageID{Kind: "theme", Slug: "aurora", Version: "1.2.1"}, "h1:abc=")
	if err := Verify(key.Public(), changed, sig); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("signature verified for a different version: %v", err)
	}
	// The same fields under another purpose are a different statement.
	asMarket := Statement{Purpose: PurposePackageMarket, Fields: statement.Fields}
	if err := Verify(key.Public(), asMarket, sig); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("publisher signature verified as a market statement: %v", err)
	}
	if err := Verify(other.Public(), statement, sig); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("signature verified with another key: %v", err)
	}
	forged := sig
	forged.KID = other.Public().KID()
	if err := Verify(other.Public(), statement, forged); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("relabelled signature verified: %v", err)
	}
	wrongAlg := sig
	wrongAlg.Alg = "rsa"
	if err := Verify(key.Public(), statement, wrongAlg); err == nil {
		t.Fatal("unsupported algorithm accepted")
	}

	trusted := NewKeySet(key.Public())
	if err := trusted.Verify(statement, sig); err != nil {
		t.Fatalf("KeySet rejected a trusted signature: %v", err)
	}
	otherSig, _ := other.Sign(statement)
	if err := trusted.Verify(statement, otherSig); !errors.Is(err, ErrUnknownKey) {
		t.Fatalf("KeySet accepted an untrusted key: %v", err)
	}
	parsed, err := ParseKeySet([]string{key.Public().String()})
	if err != nil || len(parsed) != 1 || parsed.Verify(statement, sig) != nil {
		t.Fatalf("ParseKeySet = %v, %v", parsed, err)
	}
}

func TestDigestMatchesKnownVectorAndIgnoresSignatures(t *testing.T) {
	files := fstest.MapFS{
		"dir/b.txt": {Data: []byte("world")},
		"a.txt":     {Data: []byte("hello\n")},
	}
	// Computed independently (Python) from the h1 definition:
	// base64(sha256("<sha256 a.txt>  a.txt\n<sha256 dir/b.txt>  dir/b.txt\n")).
	const want = "h1:dQkzgOEPNsZ3VPp6O8BBw73dxpLZpWDCWxswNbGkIN4="
	got, err := PackageDigest(files)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("PackageDigest = %s, want %s", got, want)
	}

	files["signatures/publisher.json"] = &fstest.MapFile{Data: []byte(`{"kid":"x"}`)}
	if got, _ := PackageDigest(files); got != want {
		t.Fatal("signatures/ must not change the digest")
	}
	files["templates/signatures/x.tmpl"] = &fstest.MapFile{Data: []byte("x")}
	if got, _ := PackageDigest(files); got == want {
		t.Fatal("only the top-level signatures/ is excluded")
	}
	delete(files, "templates/signatures/x.tmpl")
	files["a.txt"] = &fstest.MapFile{Data: []byte("hello!\n")}
	if got, _ := PackageDigest(files); got == want {
		t.Fatal("changing a file must change the digest")
	}

	if _, err := PackageDigest(fstest.MapFS{"signatures/a.json": {Data: []byte("{}")}}); err == nil {
		t.Fatal("a package with only signatures must not digest")
	}
}

func TestDigestOfDirectoryEqualsDigestOfZip(t *testing.T) {
	dir := t.TempDir()
	write := func(name, data string) {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("theme.toml", "[theme]\nname = \"A\"\n")
	write("templates/layouts/base.tmpl", "{{define \"base\"}}x{{end}}")
	write("static/css/a.css", "body{}")
	dirDigest, err := PackageDigest(os.DirFS(dir))
	if err != nil {
		t.Fatal(err)
	}

	// Different order, explicit directory entries, and compression: same digest.
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, entry := range []struct{ name, data string }{
		{"static/", ""},
		{"static/css/a.css", "body{}"},
		{"templates/layouts/base.tmpl", "{{define \"base\"}}x{{end}}"},
		{"theme.toml", "[theme]\nname = \"A\"\n"},
	} {
		w, err := zw.Create(entry.name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(entry.data))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	zipDigest, err := PackageDigest(zr)
	if err != nil {
		t.Fatal(err)
	}
	if zipDigest != dirDigest {
		t.Fatalf("zip digest %s != directory digest %s", zipDigest, dirDigest)
	}

	if err := os.Symlink(filepath.Join(dir, "theme.toml"), filepath.Join(dir, "link.toml")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := PackageDigest(os.DirFS(dir)); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("symlink accepted: %v", err)
	}
}

// signedPackage returns a package signed by publisher and, when market is
// not nil, countersigned by market.
func signedPackage(t *testing.T, id PackageID, publisher PrivateKey, market *PrivateKey) fstest.MapFS {
	t.Helper()
	files := fstest.MapFS{
		"theme.toml":             {Data: []byte("[theme]\nslug = \"" + id.Slug + "\"\n")},
		"templates/pages/a.tmpl": {Data: []byte("a")},
	}
	digest, err := PackageDigest(files)
	if err != nil {
		t.Fatal(err)
	}
	pubSig, err := publisher.Sign(PublisherStatement(id, digest))
	if err != nil {
		t.Fatal(err)
	}
	pubSig.PublicKey = publisher.Public().String()
	data, _ := MarshalSignature(pubSig)
	files[PublisherSignatureFile] = &fstest.MapFile{Data: data}
	if market != nil {
		marketSig, err := market.Sign(MarketStatement(id, publisher.Public().KID(), digest))
		if err != nil {
			t.Fatal(err)
		}
		data, _ := MarshalSignature(marketSig)
		files[MarketSignatureFile] = &fstest.MapFile{Data: data}
	}
	return files
}

func TestVerifyPackage(t *testing.T) {
	id := PackageID{Kind: "theme", Slug: "aurora", Version: "1.2.0"}
	publisher, market, stranger := mustKey(t), mustKey(t), mustKey(t)
	trusted := NewKeySet(market.Public())

	files := signedPackage(t, id, publisher, &market)
	result, err := VerifyPackage(files, id, VerifyOptions{MarketKeys: trusted, RequireMarket: true})
	if err != nil {
		t.Fatalf("valid package rejected: %v", err)
	}
	if !result.Market || result.PublisherKID != publisher.Public().KID() || !strings.HasPrefix(result.Digest, "h1:") {
		t.Fatalf("result = %+v", result)
	}

	// The catalog vouches for the publisher key.
	if _, err := VerifyPackage(files, id, VerifyOptions{PublisherKey: publisher.Public()}); err != nil {
		t.Fatalf("catalog publisher key rejected: %v", err)
	}
	if _, err := VerifyPackage(files, id, VerifyOptions{PublisherKey: stranger.Public()}); err == nil {
		t.Fatal("package accepted for a publisher key the catalog does not name")
	}

	cases := map[string]struct {
		files fstest.MapFS
		id    PackageID
		opts  VerifyOptions
		want  error
	}{
		"unsigned": {
			files: fstest.MapFS{"theme.toml": {Data: []byte("x")}},
			id:    id, opts: VerifyOptions{}, want: ErrUnsigned,
		},
		"tampered file": {
			files: func() fstest.MapFS {
				f := signedPackage(t, id, publisher, &market)
				f["templates/pages/a.tmpl"] = &fstest.MapFile{Data: []byte("evil")}
				return f
			}(),
			id: id, opts: VerifyOptions{MarketKeys: trusted},
		},
		"other version": {
			files: files, id: PackageID{Kind: "theme", Slug: "aurora", Version: "9.9.9"},
			opts: VerifyOptions{},
		},
		"untrusted market key": {
			files: signedPackage(t, id, publisher, &stranger), id: id,
			opts: VerifyOptions{MarketKeys: trusted}, want: ErrUnknownKey,
		},
		"countersignature for another publisher": {
			files: func() fstest.MapFS {
				f := signedPackage(t, id, publisher, nil)
				digest, _ := PackageDigest(f)
				sig, _ := market.Sign(MarketStatement(id, stranger.Public().KID(), digest))
				data, _ := MarshalSignature(sig)
				f[MarketSignatureFile] = &fstest.MapFile{Data: data}
				return f
			}(),
			id: id, opts: VerifyOptions{MarketKeys: trusted}, want: ErrBadSignature,
		},
		"market required but missing": {
			files: signedPackage(t, id, publisher, nil), id: id,
			opts: VerifyOptions{MarketKeys: trusted, RequireMarket: true},
		},
		"market required without keys": {
			files: files, id: id, opts: VerifyOptions{RequireMarket: true}, want: ErrUnknownKey,
		},
	}
	for name, tc := range cases {
		_, err := VerifyPackage(tc.files, tc.id, tc.opts)
		if err == nil {
			t.Errorf("%s: verification passed", name)
			continue
		}
		if tc.want != nil && !errors.Is(err, tc.want) {
			t.Errorf("%s: error = %v, want %v", name, err, tc.want)
		}
	}

	// Without market keys a countersigned package still passes on the
	// publisher signature alone, and Market stays false.
	if result, err := VerifyPackage(files, id, VerifyOptions{}); err != nil || result.Market {
		t.Fatalf("publisher-only verification = %+v, %v", result, err)
	}

	// Signatures supplied by a catalog replace the files in the package.
	bare := fstest.MapFS{"theme.toml": files["theme.toml"], "templates/pages/a.tmpl": files["templates/pages/a.tmpl"]}
	pubSig, _ := ReadSignature(files, PublisherSignatureFile)
	marketSig, _ := ReadSignature(files, MarketSignatureFile)
	if _, err := VerifyPackage(bare, id, VerifyOptions{
		PublisherKey: publisher.Public(), PublisherSignature: &pubSig,
		MarketKeys: trusted, MarketSignature: &marketSig, RequireMarket: true,
	}); err != nil {
		t.Fatalf("catalog-supplied signatures rejected: %v", err)
	}
}
