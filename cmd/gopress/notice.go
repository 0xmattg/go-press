// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"time"

	"github.com/0xmattg/go-press/core/updatecheck"
	"github.com/0xmattg/go-press/pkg/sign"
)

const noticeUsage = `gopress notice — sign and verify official admin announcements

Announcement bundles are signed offline with keys whose public half is built
into GoPress; the update server only stores signed bundles. Keep the private
keys off the server.

Usage:
  gopress notice keygen -out <prefix> [-force]
        Create an Ed25519 key pair: <prefix>.key (private, mode 0600) and
        <prefix>.pub (public, base64). Prints the key ID.
  gopress notice sign -key <file.key> [-key <file.key>] [-out <file>] <bundle.json>
        Check a bundle document (schema, sequence, generated_at, expires_at,
        notices) the way sites do and wrap it in a signed envelope (one or
        two keys). Refuses a bundle with any announcement sites would drop.
        Writes to stdout without -out.
  gopress notice verify [-pub <base64>]... <signed.json>
        Verify a signed bundle with the keys built into this GoPress, or
        with -pub, and summarize it. Exits 1 if verification fails.
`

// runNotice implements "gopress notice". It needs no repository checkout.
func runNotice(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		fmt.Fprint(stdout, noticeUsage)
		if len(args) == 0 {
			return 2
		}
		return 0
	}
	var err error
	switch args[0] {
	case "keygen":
		err = pkgKeygen(args[1:], stdout)
	case "sign":
		err = noticeSign(args[1:], stdout, time.Now())
	case "verify":
		err = noticeVerify(args[1:], stdout, time.Now())
	default:
		fmt.Fprintf(stderr, "gopress notice: unknown command %q\n\n%s", args[0], noticeUsage)
		return 2
	}
	var usage usageError
	switch {
	case errors.As(err, &usage):
		fmt.Fprintf(stderr, "gopress notice %s: %v\n", args[0], err)
		return 2
	case err != nil:
		fmt.Fprintf(stderr, "gopress notice %s: %v\n", args[0], err)
		return 1
	}
	return 0
}

func noticeSign(args []string, stdout io.Writer, now time.Time) error {
	flags := newFlagSet("notice sign")
	var keyPaths keyList
	flags.Var(&keyPaths, "key", "announcement private key file; repeatable (at most 2)")
	out := flags.String("out", "", "output file (default: stdout)")
	if err := flags.Parse(args); err != nil {
		return usageError{err.Error()}
	}
	if len(keyPaths) == 0 || flags.NArg() != 1 {
		return usageError{"want -key <file> and one bundle document"}
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
	env, err := updatecheck.SignNotices(payload, now, keys...)
	if err != nil {
		return err
	}
	data, err := updatecheck.MarshalSignedNotices(env)
	if err != nil {
		return err
	}
	if *out == "" {
		_, err := stdout.Write(data)
		return err
	}
	if err := writeFile(*out, data, 0o644); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "wrote %s (%d signature(s))\n", *out, len(keys))
	return nil
}

func noticeVerify(args []string, stdout io.Writer, now time.Time) error {
	flags := newFlagSet("notice verify")
	var pubs keyList
	flags.Var(&pubs, "pub", "trusted announcement public key (base64); repeatable; default: the keys built into GoPress")
	if err := flags.Parse(args); err != nil {
		return usageError{err.Error()}
	}
	if flags.NArg() != 1 {
		return usageError{"want one signed bundle file"}
	}
	keys := updatecheck.BuiltinNoticeKeys()
	if len(pubs) > 0 {
		var err error
		if keys, err = sign.ParseKeySet(pubs); err != nil {
			return err
		}
	}
	if len(keys) == 0 {
		return errors.New("this GoPress has no built-in announcement keys yet; pass -pub")
	}
	raw, err := os.ReadFile(flags.Arg(0))
	if err != nil {
		return err
	}
	var env updatecheck.SignedNotices
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("%s is not a signed bundle: %w", flags.Arg(0), err)
	}
	bundle, err := updatecheck.ParseNotices(env, keys, now)
	if err != nil && !errors.Is(err, updatecheck.ErrNoticesExpired) {
		return err
	}
	kids := make([]string, 0, len(env.Signatures))
	for _, sig := range env.Signatures {
		kids = append(kids, sig.KID)
	}
	sort.Strings(kids)
	fmt.Fprintf(stdout, "bundle:    ok, sequence %d, signed by %v\ngenerated: %s\nexpires:   %s\n",
		bundle.Sequence, kids, bundle.GeneratedAt.Format(time.RFC3339), bundle.ExpiresAt.Format(time.RFC3339))
	if err != nil {
		fmt.Fprintln(stdout, "status:    expired; sites clear their announcements when they receive it")
		return nil
	}
	fmt.Fprintf(stdout, "notices:   %d\n", len(bundle.Notices))
	for _, n := range bundle.Notices {
		fmt.Fprintf(stdout, "  %s r%d %s %s: %s\n", n.ID, n.Revision, n.Severity, n.Category, n.Title["en"])
		for _, target := range n.Affects {
			fmt.Fprintf(stdout, "    affects %s %s %s\n", target.Kind, target.Slug, target.Versions)
		}
	}
	for _, reason := range bundle.Dropped {
		fmt.Fprintf(stdout, "  dropped: %s\n", reason)
	}
	return nil
}
