package version

import (
	"github.com/hashicorp/packer-plugin-sdk/version"
)

var (
	// Version is the main version number that is being run at the moment.
	Version = "0.1.0"

	// VersionPrerelease is a pre-release marker for the version.
	VersionPrerelease = ""

	// VersionMetadata is the build metadata for the version.
	VersionMetadata = ""

	// PluginVersion is used by the plugin set to allow Packer to recognize
	// what version this plugin is.
	PluginVersion = version.NewPluginVersion(Version, VersionPrerelease, VersionMetadata)
)
