package version

import "fmt"

const (
	Major = 0  // Major version component of the current release
	Minor = 6  // Minor version component of the current release
	Patch = 9  // Patch version component of the current release
	Meta  = "" // Version metadata to append to the version string
)

func String() string {
	v := fmt.Sprintf("%d.%d.%d", Major, Minor, Patch)
	if Meta != "" {
		v += "-" + Meta
	}
	return v
}
