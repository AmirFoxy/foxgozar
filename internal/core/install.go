package core

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Variant is set at build time for the ARM/MIPS flavours runtime.GOARCH
// cannot tell apart: armv5, armv6, armv7, softfloat, hardfloat.
var Variant = ""

type Kind string

const (
	SingBox Kind = "sing-box"
	Xray    Kind = "xray"
)

func repo(k Kind) string {
	if k == Xray {
		return "XTLS/Xray-core"
	}
	return "SagerNet/sing-box"
}

// assetSuffix names the release file for this router.
func assetSuffix(k Kind) (string, error) {
	a := runtime.GOARCH
	if k == SingBox {
		switch a {
		case "amd64", "arm64", "riscv64", "loong64", "s390x", "ppc64le":
			return "linux-" + a + ".tar.gz", nil
		case "386":
			if Variant == "softfloat" {
				return "linux-386-softfloat.tar.gz", nil
			}
			return "linux-386.tar.gz", nil
		case "arm":
			v := Variant
			if v == "" {
				v = "armv7"
			}
			return "linux-" + v + ".tar.gz", nil
		case "mips", "mipsle", "mips64", "mips64le":
			if Variant == "hardfloat" && a != "mips" && a != "mips64" {
				return "linux-" + a + ".tar.gz", nil
			}
			return "linux-" + a + "-softfloat.tar.gz", nil
		}
	} else {
		m := map[string]string{"amd64": "64", "386": "32", "arm64": "arm64-v8a", "riscv64": "riscv64",
			"loong64": "loong64", "mips": "mips32", "mipsle": "mips32le", "mips64": "mips64",
			"mips64le": "mips64le", "ppc64le": "ppc64le", "s390x": "s390x"}
		if a == "arm" {
			switch Variant {
			case "armv5":
				return "linux-arm32-v5.zip", nil
			case "armv6":
				return "linux-arm32-v6.zip", nil
			}
			return "linux-arm32-v7a.zip", nil
		}
		if s, ok := m[a]; ok {
			return "linux-" + s + ".zip", nil
		}
	}
	return "", fmt.Errorf("no %s build for %s/%s", k, a, Variant)
}

// Fetcher downloads, trying each client in turn: through the tunnel when it
// is up (GitHub may be slow or filtered), then direct.
type Fetcher struct {
	Clients []*http.Client
	Mirror  string // optional prefix, e.g. https://ghproxy.example/
	UA      string
}

func HTTPClient(proxyPort int, timeout time.Duration) *http.Client {
	tr := &http.Transport{Proxy: nil, TLSHandshakeTimeout: 15 * time.Second, ResponseHeaderTimeout: 30 * time.Second}
	if proxyPort > 0 {
		u, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", proxyPort))
		tr.Proxy = http.ProxyURL(u)
	}
	return &http.Client{Transport: tr, Timeout: timeout}
}

func (f *Fetcher) Get(ctx context.Context, rawURL string, limit int64) ([]byte, error) {
	b, _, err := f.GetUA(ctx, rawURL, f.UA, limit)
	return b, err
}

// GetUA is Get with a chosen User-Agent, also returning the headers.
func (f *Fetcher) GetUA(ctx context.Context, rawURL, ua string, limit int64) ([]byte, http.Header, error) {
	urls := []string{rawURL}
	if f.Mirror != "" && strings.Contains(rawURL, "github") {
		urls = append(urls, strings.TrimSuffix(f.Mirror, "/")+"/"+rawURL)
	}
	var errs []string
	for _, c := range f.Clients {
		for _, u := range urls {
			b, h, err := get(ctx, c, u, ua, limit)
			if err == nil {
				return b, h, nil
			}
			errs = append(errs, err.Error())
			if ctx.Err() != nil {
				return nil, nil, ctx.Err()
			}
		}
	}
	return nil, nil, errors.New(strings.Join(errs, "; "))
}

func get(ctx context.Context, c *http.Client, u, ua string, limit int64) ([]byte, http.Header, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, nil, err
	}
	if ua == "" {
		ua = "foxgozar"
	}
	req.Header.Set("User-Agent", ua)
	resp, err := c.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, nil, fmt.Errorf("%s: HTTP %d", u, resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, nil, err
	}
	if int64(len(b)) > limit {
		return nil, nil, fmt.Errorf("%s: too large", u)
	}
	return b, resp.Header, nil
}

type Release struct {
	Tag    string `json:"tag_name"`
	Pre    bool   `json:"prerelease"`
	Assets []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

// Latest finds the newest release; beta includes pre-releases.
func (f *Fetcher) Latest(ctx context.Context, k Kind, beta bool) (Release, error) {
	b, err := f.Get(ctx, "https://api.github.com/repos/"+repo(k)+"/releases?per_page=10", 4<<20)
	if err != nil {
		return Release{}, err
	}
	var rs []Release
	if err := json.Unmarshal(b, &rs); err != nil {
		return Release{}, err
	}
	for _, r := range rs {
		if beta || !r.Pre {
			return r, nil
		}
	}
	return Release{}, errors.New("no release found")
}

// Install downloads release r of core k into dir and returns the binary path.
// The old binary keeps working until the new one is fully in place.
func (f *Fetcher) Install(ctx context.Context, k Kind, r Release, dir string) (string, error) {
	suffix, err := assetSuffix(k)
	if err != nil {
		return "", err
	}
	var assetURL string
	for _, a := range r.Assets {
		if strings.HasSuffix(a.Name, suffix) && (k == Xray || strings.HasPrefix(a.Name, "sing-box-")) {
			assetURL = a.URL
			break
		}
	}
	if assetURL == "" {
		return "", fmt.Errorf("release %s has no asset *%s", r.Tag, suffix)
	}
	data, err := f.Get(ctx, assetURL, 200<<20)
	if err != nil {
		return "", err
	}
	want := string(k)
	var bin []byte
	if strings.HasSuffix(suffix, ".zip") {
		bin, err = fromZip(data, want)
	} else {
		bin, err = fromTarGz(data, want)
	}
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	dst := filepath.Join(dir, want)
	tmp := dst + ".new"
	if err := os.WriteFile(tmp, bin, 0o755); err != nil {
		os.Remove(tmp)
		return "", err
	}
	// Refuse a binary that does not run here (wrong float ABI and the like).
	if out, err := exec.Command(tmp, "version").CombinedOutput(); err != nil {
		os.Remove(tmp)
		return "", fmt.Errorf("downloaded %s does not run on this router: %v %s", want, err, bytes.TrimSpace(out))
	}
	return dst, os.Rename(tmp, dst)
}

func fromTarGz(data []byte, name string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err != nil {
			return nil, fmt.Errorf("%s not in archive: %v", name, err)
		}
		if h.Typeflag == tar.TypeReg && path.Base(h.Name) == name {
			return io.ReadAll(io.LimitReader(tr, 200<<20))
		}
	}
}

func fromZip(data []byte, name string) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	for _, f := range zr.File {
		if path.Base(f.Name) == name {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return io.ReadAll(io.LimitReader(rc, 200<<20))
		}
	}
	return nil, fmt.Errorf("%s not in archive", name)
}

// Version reports the installed version, "" when not installed.
func Version(bin string) string {
	out, err := exec.Command(bin, "version").Output()
	if err != nil {
		return ""
	}
	first := strings.SplitN(string(out), "\n", 2)[0]
	for _, w := range strings.Fields(first) {
		if len(w) > 0 && (w[0] >= '0' && w[0] <= '9' || w[0] == 'v' && len(w) > 1 && w[1] >= '0' && w[1] <= '9') {
			return strings.TrimPrefix(w, "v")
		}
	}
	return strings.TrimSpace(first)
}
