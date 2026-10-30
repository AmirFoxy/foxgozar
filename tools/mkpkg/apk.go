package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// stageAPK writes <dir>.tar.gz: the package files under root/, the install
// scripts, and mkapk.sh, which turns them into an APKv3 package (OpenWrt
// 25.12 and later) with `apk mkpkg`. That tool exists only on Linux, so the
// last step runs on the router itself or in CI:
//
//	tar -xzf arm_cortex-a7_neon-vfpv4.tar.gz && sh mkapk.sh
func stageAPK(dir, arch, version, bin string) {
	var files []file
	for _, f := range dataFiles(bin) {
		files = append(files, file{"./root/" + strings.TrimPrefix(f.path, "./"), f.mode, f.data})
	}
	scripts := map[string]string{
		"post-install":  postinst,
		"post-upgrade":  postinst,
		"pre-deinstall": prerm,
	}
	var args []string
	for _, typ := range []string{"post-install", "post-upgrade", "pre-deinstall"} {
		files = append(files, file{"./scripts/" + typ, 0o755, []byte(scripts[typ])})
		args = append(args, fmt.Sprintf("--script %s:scripts/%s", typ, typ))
	}
	// OpenWrt apk versions carry a release suffix.
	apkVersion := version
	if !strings.Contains(apkVersion, "-r") {
		apkVersion += "-r1"
	}
	out := fmt.Sprintf("foxgozar-%s.%s.apk", apkVersion, arch)
	script := fmt.Sprintf(`#!/bin/sh
# Builds %[1]s from root/ and scripts/ with apk-tools v3.
set -e
cd "$(dirname "$0")"
apk mkpkg \
	--info name:foxgozar \
	--info version:%[2]s \
	--info "description:FoxGozar - fast transparent proxy for OpenWrt (sing-box core)" \
	--info arch:%[3]s \
	--info license:AGPL-3.0-or-later \
	--info "depends:libc kmod-nft-tproxy ca-bundle" \
	%[4]s \
	--files root \
	--output %[1]s
echo %[1]s
`, out, apkVersion, arch, strings.Join(args, " \\\n\t"))
	files = append(files, file{"./mkapk.sh", 0o755, []byte(script)})
	must(os.MkdirAll(filepath.Dir(dir), 0o755))
	must(os.WriteFile(dir+".tar.gz", tgz(files), 0o644))
}
