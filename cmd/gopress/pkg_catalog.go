package main

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/0xmattg/go-press/config"
	"github.com/0xmattg/go-press/core/extension"
	"github.com/0xmattg/go-press/pkg/sign"
)

// pkgCatalog implements "gopress pkg catalog sign|verify".
func pkgCatalog(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return usageError{"want catalog sign or catalog verify"}
	}
	switch args[0] {
	case "sign":
		return pkgCatalogSign(args[1:], stdout)
	case "verify":
		return pkgCatalogVerify(args[1:], stdout)
	default:
		return usageError{fmt.Sprintf("unknown catalog command %q (sign or verify)", args[0])}
	}
}

func pkgCatalogSign(args []string, stdout io.Writer) error {
	flags := newFlagSet("catalog sign")
	var keyPaths keyList
	flags.Var(&keyPaths, "key", "catalog private key file; repeatable")
	out := flags.String("out", "", "output file (default: stdout)")
	if err := flags.Parse(args); err != nil {
		return usageError{err.Error()}
	}
	if len(keyPaths) == 0 || flags.NArg() != 1 {
		return usageError{"want -key <file> and one catalog payload file"}
	}
	payload, err := os.ReadFile(flags.Arg(0))
	if err != nil {
		return err
	}
	keys := make([]sign.PrivateKey, 0, len(keyPaths))
	for _, path := range keyPaths {
		pemData, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		key, err := sign.ParsePrivateKeyPEM(pemData)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		keys = append(keys, key)
	}
	envelope, err := extension.SignCatalog(payload, keys...)
	if err != nil {
		return err
	}
	if *out == "" {
		_, err := stdout.Write(envelope)
		return err
	}
	if err := writeFile(*out, envelope, 0o644); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "wrote %s (%d signature(s))\n", *out, len(keys))
	return nil
}

func pkgCatalogVerify(args []string, stdout io.Writer) error {
	flags := newFlagSet("catalog verify")
	var catalogKeys, packageKeys keyList
	flags.Var(&catalogKeys, "catalog-key", "trusted catalog public key (base64); repeatable")
	flags.Var(&packageKeys, "package-key", "trusted market package public key (base64); repeatable")
	endpoint := flags.String("endpoint", extension.DefaultMarketEndpoint, "market base URL (a loopback http URL also allows loopback download URLs)")
	if err := flags.Parse(args); err != nil {
		return usageError{err.Error()}
	}
	if len(catalogKeys) == 0 || len(packageKeys) == 0 || flags.NArg() != 1 {
		return usageError{"want -catalog-key, -package-key and one catalog file"}
	}
	root, err := extension.NewTrustRoot(config.MarketConfig{Endpoint: *endpoint, CatalogKeys: catalogKeys, PackageKeys: packageKeys})
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(flags.Arg(0))
	if err != nil {
		return err
	}
	cat, err := extension.ParseCatalog(raw, root, time.Now())
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "catalog:   ok, sequence %d\ngenerated: %s\nexpires:   %s\n",
		cat.Sequence, cat.GeneratedAt.UTC().Format(time.RFC3339), cat.ExpiresAt.UTC().Format(time.RFC3339))
	fmt.Fprintf(stdout, "items:     %d\n", len(cat.Items))
	for _, item := range cat.Items {
		compat := ""
		if !item.Compatible {
			compat = " (needs GoPress " + item.Core + ")"
		}
		fmt.Fprintf(stdout, "  %s %s %s %s%s\n", item.Kind, item.Slug, item.Version, item.Pricing, compat)
	}
	if len(cat.Hidden) > 0 {
		fmt.Fprintf(stdout, "hidden:    %d (signatures do not verify)\n", len(cat.Hidden))
		for _, id := range cat.Hidden {
			fmt.Fprintf(stdout, "  %s\n", id)
		}
	}
	fmt.Fprintf(stdout, "revoked:   %d\n", len(cat.Revoked))
	for _, rev := range cat.Revoked {
		fmt.Fprintf(stdout, "  %s %s %v %s/%s\n", rev.Kind, rev.Slug, rev.Versions, rev.Severity, rev.Reason)
	}
	return nil
}
