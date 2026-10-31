package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// Psiphon's tunnel core (ConsoleClient, built from Psiphon-Labs'
// psiphon-tunnel-core) comes from regionhop's release bundles, the one
// public place with ready Linux builds for routers. The release is pinned
// and every bundle is checked against its SHA-256 before anything is used.
const psiphonRelease = "https://github.com/freeb5d/regionhop/releases/download/v1.26.0/"

var psiphonBundles = map[string]struct{ file, sha256 string }{
	"amd64": {"regionhop-linux-amd64.tar.gz", "8d2fb35c40a82f3e9f20713f8ce1120799b7d55d4f39c66890205a286f3c9e56"},
	"arm64": {"regionhop-linux-arm64.tar.gz", "71f8cc4ff1e0ef52d47f8ba0595bcddcf1785b8e79e7db768083a81300728327"},
	"armv7": {"regionhop-linux-armv7.tar.gz", "42fd6e6ebd4ba6cf3eb543a7312f2c15415e7fbc6b2da346b6f91131299ccc9b"},
}

// PsiphonArch is the bundle for this router, or "" when there is none.
func PsiphonArch() string {
	switch {
	case runtime.GOARCH == "amd64", runtime.GOARCH == "arm64":
		return runtime.GOARCH
	case runtime.GOARCH == "arm" && Variant == "armv7":
		return "armv7"
	}
	return ""
}

// InstallPsiphon puts the Psiphon tunnel core at dir/psiphon.
func (f *Fetcher) InstallPsiphon(ctx context.Context, dir string) error {
	arch := PsiphonArch()
	b, ok := psiphonBundles[arch]
	if !ok {
		return fmt.Errorf("there is no Psiphon build for this router (%s %s); it exists for x86_64, arm64 and armv7", runtime.GOARCH, Variant)
	}
	data, err := f.Get(ctx, psiphonRelease+b.file, 64<<20)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != b.sha256 {
		return fmt.Errorf("the Psiphon download does not match its checksum; not installed")
	}
	bin, err := fromTarGz(data, "ConsoleClient")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp := filepath.Join(dir, ".psiphon.tmp")
	if err := os.WriteFile(tmp, bin, 0o755); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, "psiphon"))
}
