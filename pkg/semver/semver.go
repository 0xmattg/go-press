// Package semver wraps Masterminds/semver with the small surface GoPress needs:
// validating extension (theme/plugin) versions and checking the version
// constraints declared in a theme's [requires] block. Kept as a thin façade so
// the rest of the codebase never imports the third-party library directly.
package semver

import (
	"fmt"

	"github.com/Masterminds/semver/v3"
)

// Valid reports whether v parses as a version (e.g. "1.2.0", "v1.2.0", "1.2").
func Valid(v string) bool {
	_, err := semver.NewVersion(v)
	return err == nil
}

// ValidConstraint reports whether c parses as a version constraint, e.g.
// ">=1.2.0", "^1.0", "~1.2", or ">=1.2 <2.0 || >=3.0".
func ValidConstraint(c string) bool {
	_, err := semver.NewConstraint(c)
	return err == nil
}

// Satisfies reports whether version satisfies constraint. It returns an error
// when either the version or the constraint fails to parse, so callers can tell
// "does not satisfy" apart from "malformed input".
func Satisfies(version, constraint string) (bool, error) {
	v, err := semver.NewVersion(version)
	if err != nil {
		return false, fmt.Errorf("invalid version %q: %w", version, err)
	}
	c, err := semver.NewConstraint(constraint)
	if err != nil {
		return false, fmt.Errorf("invalid constraint %q: %w", constraint, err)
	}
	return c.Check(v), nil
}

// Compare returns -1, 0, or 1 as a is less than, equal to, or greater than b.
func Compare(a, b string) (int, error) {
	va, err := semver.NewVersion(a)
	if err != nil {
		return 0, fmt.Errorf("invalid version %q: %w", a, err)
	}
	vb, err := semver.NewVersion(b)
	if err != nil {
		return 0, fmt.Errorf("invalid version %q: %w", b, err)
	}
	return va.Compare(vb), nil
}
