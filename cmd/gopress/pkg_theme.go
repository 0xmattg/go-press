package main

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/0xmattg/go-press/core/extension/themepkg"
	"github.com/0xmattg/go-press/pkg/sign"
)

// errCheckFailed reports a package that failed the check; the report has
// already been printed.
var errCheckFailed = errors.New("the package did not pass the check")

func pkgCheck(args []string, stdout io.Writer) error {
	flags := newFlagSet("check")
	coreVersion := flags.String("core", "", "GoPress version the package must support")
	if err := flags.Parse(args); err != nil {
		return usageError{err.Error()}
	}
	if flags.NArg() != 1 {
		return usageError{"want exactly one theme directory or .zip file"}
	}
	target := flags.Arg(0)
	info, err := os.Stat(target)
	if err != nil {
		return err
	}
	opts := themepkg.Options{CoreVersion: *coreVersion}
	var report *themepkg.Report
	if info.IsDir() {
		report, err = themepkg.CheckDir(os.DirFS(target), opts)
	} else {
		report, err = checkArchive(target, opts)
	}
	if err != nil {
		return err
	}
	printReport(stdout, report)
	if !report.OK() {
		return errCheckFailed
	}
	return nil
}

func checkArchive(path string, opts themepkg.Options) (*themepkg.Report, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	return themepkg.CheckZip(f, info.Size(), opts)
}

func pkgBuild(args []string, stdout io.Writer) error {
	flags := newFlagSet("build")
	coreVersion := flags.String("core", "", "GoPress version the package must support")
	out := flags.String("out", "", "archive to write (default <slug>-<version>.zip)")
	if err := flags.Parse(args); err != nil {
		return usageError{err.Error()}
	}
	if flags.NArg() != 1 {
		return usageError{"want exactly one theme source directory"}
	}
	dir := flags.Arg(0)
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return fmt.Errorf("%s is not a directory", dir)
	}
	var archive bytes.Buffer
	report, err := themepkg.Build(&archive, os.DirFS(dir), themepkg.Options{CoreVersion: *coreVersion})
	if err != nil {
		return err
	}
	printReport(stdout, report)
	if !report.OK() {
		return errCheckFailed
	}
	path := *out
	if path == "" {
		path = fmt.Sprintf("%s-%s.zip", report.ID.Slug, report.ID.Version)
	}
	if err := writeAtomic(path, archive.Bytes()); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "wrote %s (%s)\n", path, sizeLabel(int64(archive.Len())))
	return nil
}

// printReport prints a check report: identity, digest, size, external hosts,
// then problems and warnings grouped by rule.
func printReport(w io.Writer, r *themepkg.Report) {
	fmt.Fprintf(w, "package: %s %s %s\n", r.ID.Kind, orDash(r.ID.Slug), orDash(r.ID.Version))
	if r.Digest != "" {
		fmt.Fprintf(w, "digest:  %s\n", r.Digest)
	}
	fmt.Fprintf(w, "files:   %d (%s)\n", r.Files, sizeLabel(r.Size))
	for _, ext := range r.External {
		fmt.Fprintf(w, "external: %s %v (%s)\n", ext.Host, ext.Kinds, ext.Purpose)
	}
	printProblems(w, "problems", r.Problems, r.Omitted)
	printProblems(w, "warnings", r.Warnings, 0)
	if r.OK() {
		fmt.Fprintln(w, "check:   ok")
	}
}

func printProblems(w io.Writer, title string, problems []themepkg.Problem, omitted int) {
	if len(problems) == 0 {
		return
	}
	sorted := append([]themepkg.Problem(nil), problems...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Rule < sorted[j].Rule })
	fmt.Fprintf(w, "%s (%d):\n", title, len(problems)+omitted)
	rule := ""
	for _, p := range sorted {
		if p.Rule != rule {
			rule = p.Rule
			fmt.Fprintf(w, "  %s\n", rule)
		}
		where := p.Path
		if where != "" && p.Line > 0 {
			where = fmt.Sprintf("%s:%d", where, p.Line)
		}
		if where != "" {
			where += ": "
		}
		fmt.Fprintf(w, "    %s%s\n", where, p.Message)
	}
	if omitted > 0 {
		fmt.Fprintf(w, "  … %d more\n", omitted)
	}
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func sizeLabel(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d bytes", n)
	}
}

// signPackage signs a package for role and returns the signature file's
// path inside the package and the signature.
func signPackage(fsys fs.FS, id sign.PackageID, digest string, key sign.PrivateKey, role string) (string, sign.Signature, error) {
	if role == "publisher" {
		sig, err := key.Sign(sign.PublisherStatement(id, digest))
		if err != nil {
			return "", sig, err
		}
		sig.PublicKey = key.Public().String()
		return sign.PublisherSignatureFile, sig, nil
	}
	verified, err := sign.VerifyPackage(fsys, id, sign.VerifyOptions{})
	if err != nil {
		return "", sign.Signature{}, fmt.Errorf("refusing to countersign: %w", err)
	}
	sig, err := key.Sign(sign.MarketStatement(id, verified.PublisherKID, digest))
	return sign.MarketSignatureFile, sig, err
}

// signArchive signs a .zip package and rewrites it with the signature file
// added. Signature files are outside the digest, so the digest does not
// change.
func signArchive(path string, key sign.PrivateKey, role string, force bool, identity identityFlags, stdout io.Writer) error {
	archive, err := zip.OpenReader(path)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	files, readErr := readArchive(&archive.Reader)
	id, idErr := identity.resolve(archive, "")
	digest, digestErr := sign.PackageDigest(archive)
	var sigFile string
	var sig sign.Signature
	var signErr error
	if readErr == nil && idErr == nil && digestErr == nil {
		sigFile, sig, signErr = signPackage(archive, id, digest, key, role)
	}
	_ = archive.Close()
	if err := errors.Join(readErr, idErr, digestErr, signErr); err != nil {
		return err
	}

	data, err := sign.MarshalSignature(sig)
	if err != nil {
		return err
	}
	replaced := false
	for i := range files {
		if files[i].Name == sigFile {
			if !force {
				return fmt.Errorf("%s already has %s (use -force to replace it)", path, sigFile)
			}
			files[i].Data, replaced = data, true
		}
	}
	if !replaced {
		files = append(files, themepkg.File{Name: sigFile, Data: data})
	}
	var out bytes.Buffer
	if err := themepkg.WriteZip(&out, files); err != nil {
		return err
	}
	if err := writeAtomic(path, out.Bytes()); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "signed %s %s %s as %s\ndigest: %s\nkid:    %s\nwrote %s into %s\n", id.Kind, id.Slug, id.Version, role, digest, sig.KID, sigFile, path)
	return nil
}

// readArchive reads every file of an archive into memory.
func readArchive(archive *zip.Reader) ([]themepkg.File, error) {
	var files []themepkg.File
	seen := map[string]bool{}
	for _, entry := range archive.File {
		if entry.Mode().IsDir() {
			continue
		}
		if !entry.Mode().IsRegular() || seen[entry.Name] {
			return nil, fmt.Errorf("%s: not a regular file or listed twice; rebuild the package with gopress pkg build", entry.Name)
		}
		seen[entry.Name] = true
		rc, err := entry.Open()
		if err != nil {
			return nil, err
		}
		data, err := io.ReadAll(io.LimitReader(rc, themepkg.MaxFileSize+1))
		_ = rc.Close()
		if err != nil {
			return nil, err
		}
		if len(data) > themepkg.MaxFileSize {
			return nil, fmt.Errorf("%s is larger than a package file may be", entry.Name)
		}
		files = append(files, themepkg.File{Name: entry.Name, Data: data})
	}
	return files, nil
}

// writeAtomic writes data to path through a temporary file in the same
// directory, so a failed write leaves any existing file intact.
func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".gopress-pkg-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
