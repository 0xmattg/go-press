package main

import (
	"archive/zip"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/0xmattg/go-press/pkg/sign"
)

const pkgUsage = `gopress pkg — sign and verify extension packages

Usage:
  gopress pkg keygen -out <prefix> [-force]
        Create an Ed25519 key pair: <prefix>.key (private, mode 0600) and
        <prefix>.pub (public, base64). Prints the key ID.
  gopress pkg digest <dir|file.zip>
        Print the package's h1 content digest (signatures/ excluded).
  gopress pkg sign -key <file.key> [-role publisher|market] [-force]
                   [-kind theme] [-slug s] [-version v] <dir>
        Sign a package directory and write signatures/<role>.json.
        A market countersignature requires a valid publisher signature.
  gopress pkg verify [-market-key <base64>]... [-require-market]
                     [-kind theme] [-slug s] [-version v] <dir|file.zip>
        Verify the publisher signature and, with -market-key, the market
        countersignature. Exits 1 if verification fails.

  gopress pkg catalog sign -key <file.key> [-key <file.key>]... [-out <file>] <payload.json>
        Wrap catalog JSON in a signed catalog envelope (1 to 4 keys), for a
        local or self-hosted market. Writes to stdout without -out.
  gopress pkg catalog verify -catalog-key <base64>... -package-key <base64>...
                             [-endpoint <url>] <catalog.json>
        Verify a catalog envelope the way a site does and summarize it.
        Exits 1 if verification fails.

The package identity defaults to [theme] slug and version in theme.toml;
for a directory without a slug the directory name is used.
`

// runPkg implements "gopress pkg". It needs no repository checkout.
func runPkg(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		fmt.Fprint(stdout, pkgUsage)
		if len(args) == 0 {
			return 2
		}
		return 0
	}
	var err error
	switch args[0] {
	case "keygen":
		err = pkgKeygen(args[1:], stdout)
	case "digest":
		err = pkgDigest(args[1:], stdout)
	case "sign":
		err = pkgSign(args[1:], stdout)
	case "verify":
		err = pkgVerify(args[1:], stdout)
	case "catalog":
		err = pkgCatalog(args[1:], stdout)
	default:
		fmt.Fprintf(stderr, "gopress pkg: unknown command %q\n\n%s", args[0], pkgUsage)
		return 2
	}
	var usage usageError
	switch {
	case errors.As(err, &usage):
		fmt.Fprintf(stderr, "gopress pkg %s: %v\n", args[0], err)
		return 2
	case err != nil:
		fmt.Fprintf(stderr, "gopress pkg %s: %v\n", args[0], err)
		return 1
	}
	return 0
}

type usageError struct{ msg string }

func (e usageError) Error() string { return e.msg }

func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet("gopress pkg "+name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}

func pkgKeygen(args []string, stdout io.Writer) error {
	flags := newFlagSet("keygen")
	out := flags.String("out", "", "path prefix for <prefix>.key and <prefix>.pub")
	force := flags.Bool("force", false, "overwrite existing files")
	if err := flags.Parse(args); err != nil {
		return usageError{err.Error()}
	}
	if *out == "" || flags.NArg() != 0 {
		return usageError{"want -out <prefix> and no other arguments"}
	}
	keyPath, pubPath := *out+".key", *out+".pub"
	if !*force {
		for _, path := range []string{keyPath, pubPath} {
			if _, err := os.Stat(path); err == nil {
				return fmt.Errorf("%s already exists (use -force to overwrite)", path)
			}
		}
	}
	key, err := sign.GenerateKey(nil)
	if err != nil {
		return err
	}
	pemData, err := key.MarshalPEM()
	if err != nil {
		return err
	}
	if err := writeFile(keyPath, pemData, 0o600); err != nil {
		return err
	}
	pub := key.Public()
	if err := writeFile(pubPath, []byte(pub.String()+"\n"), 0o644); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "kid:        %s\npublic key: %s\nwrote %s (keep it private) and %s\n", pub.KID(), pub, keyPath, pubPath)
	return nil
}

func pkgDigest(args []string, stdout io.Writer) error {
	if len(args) != 1 {
		return usageError{"want exactly one package directory or .zip file"}
	}
	fsys, closeFn, _, err := openPackage(args[0])
	if err != nil {
		return err
	}
	defer closeFn()
	digest, err := sign.PackageDigest(fsys)
	if err != nil {
		return err
	}
	fmt.Fprintln(stdout, digest)
	return nil
}

type identityFlags struct {
	kind, slug, version *string
}

func addIdentityFlags(flags *flag.FlagSet) identityFlags {
	return identityFlags{
		kind:    flags.String("kind", "theme", "package kind"),
		slug:    flags.String("slug", "", "package slug (default: theme.toml or directory name)"),
		version: flags.String("version", "", "package version (default: theme.toml)"),
	}
}

// resolve fills the identity from flags, then theme.toml, then dirName.
func (f identityFlags) resolve(fsys fs.FS, dirName string) (sign.PackageID, error) {
	id := sign.PackageID{Kind: *f.kind, Slug: *f.slug, Version: *f.version}
	if id.Slug == "" || id.Version == "" {
		var manifest struct {
			Theme struct {
				Slug    string `toml:"slug"`
				Version string `toml:"version"`
			} `toml:"theme"`
		}
		if data, err := fs.ReadFile(fsys, "theme.toml"); err == nil {
			if _, err := toml.Decode(string(data), &manifest); err != nil {
				return id, fmt.Errorf("theme.toml: %w", err)
			}
		}
		if id.Slug == "" {
			id.Slug = manifest.Theme.Slug
		}
		if id.Slug == "" {
			id.Slug = dirName
		}
		if id.Version == "" {
			id.Version = manifest.Theme.Version
		}
	}
	return id, id.Validate()
}

func pkgSign(args []string, stdout io.Writer) error {
	flags := newFlagSet("sign")
	keyPath := flags.String("key", "", "private key file")
	role := flags.String("role", "publisher", "publisher or market")
	force := flags.Bool("force", false, "replace an existing signature")
	identity := addIdentityFlags(flags)
	if err := flags.Parse(args); err != nil {
		return usageError{err.Error()}
	}
	if *keyPath == "" || flags.NArg() != 1 {
		return usageError{"want -key <file> and one package directory"}
	}
	if *role != "publisher" && *role != "market" {
		return usageError{fmt.Sprintf("unknown -role %q (publisher or market)", *role)}
	}
	dir := flags.Arg(0)
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return fmt.Errorf("%s is not a package directory (sign the source directory before packing)", dir)
	}
	pemData, err := os.ReadFile(*keyPath)
	if err != nil {
		return err
	}
	key, err := sign.ParsePrivateKeyPEM(pemData)
	if err != nil {
		return err
	}
	fsys := os.DirFS(dir)
	id, err := identity.resolve(fsys, filepath.Base(filepath.Clean(dir)))
	if err != nil {
		return err
	}
	digest, err := sign.PackageDigest(fsys)
	if err != nil {
		return err
	}

	target := sign.PublisherSignatureFile
	var sig sign.Signature
	if *role == "publisher" {
		if sig, err = key.Sign(sign.PublisherStatement(id, digest)); err != nil {
			return err
		}
		sig.PublicKey = key.Public().String()
	} else {
		target = sign.MarketSignatureFile
		verified, err := sign.VerifyPackage(fsys, id, sign.VerifyOptions{})
		if err != nil {
			return fmt.Errorf("refusing to countersign: %w", err)
		}
		if sig, err = key.Sign(sign.MarketStatement(id, verified.PublisherKID, digest)); err != nil {
			return err
		}
	}
	path := filepath.Join(dir, filepath.FromSlash(target))
	if _, err := os.Stat(path); err == nil && !*force {
		return fmt.Errorf("%s already exists (use -force to replace it)", path)
	}
	data, err := sign.MarshalSignature(sig)
	if err != nil {
		return err
	}
	if err := writeFile(path, data, 0o644); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "signed %s %s %s as %s\ndigest: %s\nkid:    %s\nwrote %s\n", id.Kind, id.Slug, id.Version, *role, digest, sig.KID, path)
	return nil
}

type keyList []string

func (k *keyList) String() string     { return strings.Join(*k, ",") }
func (k *keyList) Set(v string) error { *k = append(*k, v); return nil }

func pkgVerify(args []string, stdout io.Writer) error {
	flags := newFlagSet("verify")
	var marketKeys keyList
	flags.Var(&marketKeys, "market-key", "trusted market public key (base64); repeatable")
	requireMarket := flags.Bool("require-market", false, "fail unless a trusted market countersignature verifies")
	identity := addIdentityFlags(flags)
	if err := flags.Parse(args); err != nil {
		return usageError{err.Error()}
	}
	if flags.NArg() != 1 {
		return usageError{"want exactly one package directory or .zip file"}
	}
	fsys, closeFn, dirName, err := openPackage(flags.Arg(0))
	if err != nil {
		return err
	}
	defer closeFn()
	id, err := identity.resolve(fsys, dirName)
	if err != nil {
		return err
	}
	keys, err := sign.ParseKeySet(marketKeys)
	if err != nil {
		return err
	}
	result, err := sign.VerifyPackage(fsys, id, sign.VerifyOptions{MarketKeys: keys, RequireMarket: *requireMarket})
	fmt.Fprintf(stdout, "package: %s %s %s\n", id.Kind, id.Slug, id.Version)
	if result.Digest != "" {
		fmt.Fprintf(stdout, "digest:  %s\n", result.Digest)
	}
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "publisher signature: ok (kid %s)\n", result.PublisherKID)
	switch {
	case result.Market:
		fmt.Fprintln(stdout, "market signature:    ok")
	case len(keys) == 0:
		fmt.Fprintln(stdout, "market signature:    not checked (no -market-key)")
	default:
		fmt.Fprintln(stdout, "market signature:    none")
	}
	return nil
}

// openPackage opens a package directory or a .zip package as a file
// system. dirName is the directory's base name, or "" for an archive.
func openPackage(path string) (fs.FS, func(), string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, nil, "", err
	}
	if info.IsDir() {
		return os.DirFS(path), func() {}, filepath.Base(filepath.Clean(path)), nil
	}
	archive, err := zip.OpenReader(path)
	if err != nil {
		return nil, nil, "", fmt.Errorf("%s: %w", path, err)
	}
	return archive, func() { _ = archive.Close() }, "", nil
}

func writeFile(path string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, data, perm); err != nil {
		return err
	}
	return os.Chmod(path, perm)
}
