// mkpkg builds foxgozar for OpenWrt targets and packs .ipk files, without the
// OpenWrt SDK: an .ipk is a gzipped tar of debian-binary, control.tar.gz and
// data.tar.gz.
//
//	go run ./tools/mkpkg -version 6.0.0 -out dist [-arch arm_cortex-a7_neon-vfpv4,...]
package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"flag"
	"fmt"
	"github.com/dop251/goja/parser"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type target struct {
	goarch, goarm, gomips, go386, variant string
}

// OpenWrt package architectures and the Go build that runs on them.
var targets = map[string]target{
	"x86_64":                    {goarch: "amd64"},
	"i386_pentium4":             {goarch: "386", go386: "sse2"},
	"i386_pentium-mmx":          {goarch: "386", go386: "softfloat", variant: "softfloat"},
	"aarch64_generic":           {goarch: "arm64"},
	"aarch64_cortex-a53":        {goarch: "arm64"},
	"aarch64_cortex-a72":        {goarch: "arm64"},
	"aarch64_cortex-a76":        {goarch: "arm64"},
	"arm_cortex-a7":             {goarch: "arm", goarm: "7", variant: "armv7"},
	"arm_cortex-a7_neon-vfpv4":  {goarch: "arm", goarm: "7", variant: "armv7"},
	"arm_cortex-a7_vfpv4":       {goarch: "arm", goarm: "7", variant: "armv7"},
	"arm_cortex-a8_vfpv3":       {goarch: "arm", goarm: "7", variant: "armv7"},
	"arm_cortex-a9":             {goarch: "arm", goarm: "7", variant: "armv7"},
	"arm_cortex-a9_neon":        {goarch: "arm", goarm: "7", variant: "armv7"},
	"arm_cortex-a9_vfpv3-d16":   {goarch: "arm", goarm: "7", variant: "armv7"},
	"arm_cortex-a15_neon-vfpv4": {goarch: "arm", goarm: "7", variant: "armv7"},
	"arm_cortex-a5_vfpv4":       {goarch: "arm", goarm: "7", variant: "armv7"},
	"arm_arm1176jzf-s_vfp":      {goarch: "arm", goarm: "6", variant: "armv6"},
	"arm_arm926ej-s":            {goarch: "arm", goarm: "5", variant: "armv5"},
	"arm_xscale":                {goarch: "arm", goarm: "5", variant: "armv5"},
	"mipsel_24kc":               {goarch: "mipsle", gomips: "softfloat", variant: "softfloat"},
	"mipsel_24kc_24kf":          {goarch: "mipsle", gomips: "hardfloat", variant: "hardfloat"},
	"mipsel_74kc":               {goarch: "mipsle", gomips: "softfloat", variant: "softfloat"},
	"mipsel_mips32":             {goarch: "mipsle", gomips: "softfloat", variant: "softfloat"},
	"mips_24kc":                 {goarch: "mips", gomips: "softfloat", variant: "softfloat"},
	"mips_4kec":                 {goarch: "mips", gomips: "softfloat", variant: "softfloat"},
	"mips_mips32":               {goarch: "mips", gomips: "softfloat", variant: "softfloat"},
	"mips64el_mips64r2":         {goarch: "mips64le", gomips: "softfloat", variant: "softfloat"},
	"mips64_octeonplus":         {goarch: "mips64", gomips: "softfloat", variant: "softfloat"},
	"riscv64_riscv64":           {goarch: "riscv64"},
	"loongarch64_generic":       {goarch: "loong64"},
}

var buildTime = time.Now().Truncate(time.Second)

func main() {
	version := flag.String("version", "6.0.0", "package version")
	out := flag.String("out", "dist", "output directory")
	archList := flag.String("arch", "", "comma-separated OpenWrt architectures (default: all)")
	flag.Parse()

	archs := []string{}
	if *archList == "" {
		for a := range targets {
			archs = append(archs, a)
		}
	} else {
		archs = strings.Split(*archList, ",")
	}
	sort.Strings(archs)
	must(os.MkdirAll(*out, 0o755))

	built := map[target]string{} // one Go build per distinct target
	for _, arch := range archs {
		t, ok := targets[arch]
		if !ok {
			fatal("unknown architecture %q", arch)
		}
		bin, ok := built[t]
		if !ok {
			bin = filepath.Join(*out, "foxgozar-"+t.goarch+t.goarm+t.gomips+t.go386)
			build(t, *version, bin)
			built[t] = bin
		}
		name := fmt.Sprintf("foxgozar_%s_%s.ipk", *version, arch)
		must(os.WriteFile(filepath.Join(*out, name), ipk(arch, *version, bin), 0o644))
		fmt.Println(name)
		fmt.Println(writeAPK(*out, arch, *version, bin))
	}
}

func build(t target, version, out string) {
	cmd := exec.Command("go", "build", "-trimpath",
		"-ldflags", "-s -w -buildid= -X main.version="+version+" -X foxgozar/internal/core.Variant="+t.variant,
		"-o", out, "./cmd/foxgozar")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH="+t.goarch)
	if t.goarm != "" {
		cmd.Env = append(cmd.Env, "GOARM="+t.goarm)
	}
	if t.gomips != "" {
		cmd.Env = append(cmd.Env, "GOMIPS="+t.gomips, "GOMIPS64="+t.gomips)
	}
	if t.go386 != "" {
		cmd.Env = append(cmd.Env, "GO386="+t.go386)
	}
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	must(cmd.Run())
}

type file struct {
	path string
	mode int64
	data []byte
}

// readText is for scripts and JSON: a Windows checkout may have CRLF
// endings, which break the shebang line on the router.
func readText(p string) []byte {
	return bytes.ReplaceAll(readBin(p), []byte("\r\n"), []byte("\n"))
}

func readBin(p string) []byte {
	b, err := os.ReadFile(p)
	must(err)
	return b
}

// checkJS refuses to package a UI script that does not parse: one stray
// quote once left the whole page blank. LuCI wraps a view in a function,
// so it is parsed the same way.
func checkJS(path string, src []byte) {
	if _, err := parser.ParseFile(nil, path, "(function(){"+string(src)+"\n})", 0); err != nil {
		fatal("%s: %v", path, err)
	}
}

func dataFiles(bin, version string) []file {
	data := []file{
		{"./usr/bin/foxgozar", 0o755, readBin(bin)},
		{"./etc/init.d/foxgozar", 0o755, readText("files/foxgozar.init")},
		{"./usr/share/rpcd/acl.d/luci-app-foxgozar.json", 0o644, readText("files/luci/acl.json")},
		{"./usr/share/luci/menu.d/luci-app-foxgozar.json", 0o644, readText("files/luci/menu.json")},
	}
	views, _ := filepath.Glob("files/luci/view/*.js")
	for _, v := range views {
		checkJS(v, readText(v))
		data = append(data, file{"./www/luci-static/resources/view/foxgozar/" + filepath.Base(v), 0o644, bytes.ReplaceAll(readText(v), []byte("__FGZ_VERSION__"), []byte(version))})
	}
	res, _ := filepath.Glob("files/luci/res/*")
	for _, r := range res {
		b := readBin(r)
		if !strings.HasSuffix(r, ".woff2") {
			b = readText(r)
		}
		data = append(data, file{"./www/luci-static/resources/foxgozar/" + filepath.Base(r), 0o644, b})
	}
	data = append(data, file{"./usr/share/foxgozar/NOTICE", 0o644, readText("files/NOTICE")})
	// /cgi-bin/foxgozar: the web UI reaches the API through LuCI's own server.
	data = append(data, file{"./www/cgi-bin/foxgozar", 0o755, readText("files/cgi-api")})
	return data
}

const postinst = `#!/bin/sh
[ -n "$IPKG_INSTROOT" ] && exit 0
/etc/init.d/foxgozar enable
/etc/init.d/foxgozar restart
rm -rf /tmp/luci-indexcache* /tmp/luci-modulecache 2>/dev/null
/etc/init.d/rpcd reload 2>/dev/null
exit 0
`

const prerm = `#!/bin/sh
[ -n "$IPKG_INSTROOT" ] && exit 0
/etc/init.d/foxgozar stop
/etc/init.d/foxgozar disable
exit 0
`

func ipk(arch, version, bin string) []byte {
	data := dataFiles(bin, version)
	var size int
	for _, f := range data {
		size += len(f.data)
	}
	control := fmt.Sprintf(`Package: foxgozar
Version: %s
Depends: libc, kmod-nft-tproxy, ca-bundle
License: AGPL-3.0-or-later
Section: net
Architecture: %s
Installed-Size: %d
Description: FoxGozar - fast transparent proxy for OpenWrt (sing-box core)
`, version, arch, size)
	ctl := tgz([]file{
		{"./control", 0o644, []byte(control)},
		{"./postinst", 0o755, []byte(postinst)},
		{"./prerm", 0o755, []byte(prerm)},
	})
	return tgz([]file{
		{"./debian-binary", 0o644, []byte("2.0\n")},
		{"./data.tar.gz", 0o644, tgz(data)},
		{"./control.tar.gz", 0o644, ctl},
	})
}

func tgz(files []file) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	dirs := map[string]bool{}
	// Real build time: the web server sends it as Last-Modified, and an old date
	// makes browsers keep a stale app.css or app.js for weeks.
	mtime := buildTime
	for _, f := range files {
		// parent directories first, as opkg expects
		parts := strings.Split(strings.TrimPrefix(f.path, "./"), "/")
		for i := 1; i < len(parts); i++ {
			d := "./" + strings.Join(parts[:i], "/") + "/"
			if !dirs[d] {
				dirs[d] = true
				must(tw.WriteHeader(&tar.Header{Name: d, Mode: 0o755, Typeflag: tar.TypeDir, ModTime: mtime, Format: tar.FormatGNU}))
			}
		}
		must(tw.WriteHeader(&tar.Header{Name: f.path, Mode: f.mode, Size: int64(len(f.data)), Typeflag: tar.TypeReg, ModTime: mtime, Format: tar.FormatGNU}))
		_, err := tw.Write(f.data)
		must(err)
	}
	must(tw.Close())
	must(gz.Close())
	return buf.Bytes()
}

func must(err error) {
	if err != nil {
		fatal("%v", err)
	}
}

func fatal(f string, a ...any) {
	fmt.Fprintf(os.Stderr, "mkpkg: "+f+"\n", a...)
	os.Exit(1)
}
