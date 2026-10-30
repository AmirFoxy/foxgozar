package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// writeAPK builds the APKv3 package for OpenWrt 25.12 and later.
// Install with: apk add --allow-untrusted ./foxgozar_<version>_<arch>.apk
func writeAPK(out, arch, version, bin string) string {
	// OpenWrt apk versions carry a release suffix.
	apkVersion := version
	if !strings.Contains(apkVersion, "-r") {
		apkVersion += "-r1"
	}
	pkg, err := buildAPK(apkMeta{
		Name:        "foxgozar",
		Version:     apkVersion,
		Arch:        arch,
		Description: "FoxGozar - fast transparent proxy for OpenWrt (sing-box and Xray)",
		License:     "AGPL-3.0-or-later",
		Depends:     []string{"libc", "kmod-nft-tproxy", "ca-bundle"},
		Scripts: map[string]string{
			"post-install":  postinst,
			"post-upgrade":  postinst,
			"pre-deinstall": prerm,
		},
	}, dataFiles(bin, version), buildTime.Unix())
	must(err)
	name := fmt.Sprintf("foxgozar_%s_%s.apk", apkVersion, arch)
	must(os.WriteFile(filepath.Join(out, name), pkg, 0o644))
	return name
}
