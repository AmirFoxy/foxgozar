// Package warp registers a free Cloudflare WARP account and describes it as
// sing-box WireGuard endpoints, used as a backup when every server fails.
package warp

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"time"
)

// Account is what registration returns and what the endpoints need.
type Account struct {
	ID         string `json:"id"`
	Token      string `json:"token"`
	PrivateKey string `json:"private_key"`
	PeerKey    string `json:"peer_public_key"`
	Address4   string `json:"address_v4"`
	Address6   string `json:"address_v6"`
	Reserved   []int  `json:"reserved"` // from client_id; some networks need it
	Created    int64  `json:"created"`
}

const api = "https://api.cloudflareclient.com/v0a2158/reg"

// newKey makes a WireGuard (X25519) key pair, base64 encoded.
func newKey() (priv, pub string, err error) {
	k := make([]byte, 32)
	if _, err = rand.Read(k); err != nil {
		return
	}
	k[0] &= 248
	k[31] = k[31]&127 | 64
	pk, err := ecdh.X25519().NewPrivateKey(k)
	if err != nil {
		return
	}
	return base64.StdEncoding.EncodeToString(k), base64.StdEncoding.EncodeToString(pk.PublicKey().Bytes()), nil
}

// Register creates a new free account. hc decides the route: through the
// tunnel when one is up, since the API is sometimes unreachable directly.
func Register(ctx context.Context, hc *http.Client) (Account, error) {
	priv, pub, err := newKey()
	if err != nil {
		return Account{}, err
	}
	body, _ := json.Marshal(map[string]any{
		"key": pub, "install_id": "", "fcm_token": "", "warp_enabled": true,
		"tos": time.Now().UTC().Format("2006-01-02T15:04:05.000Z"), "type": "Android", "model": "PC", "locale": "en_US",
	})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, api, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json; charset=UTF-8")
	req.Header.Set("User-Agent", "okhttp/3.12.1")
	req.Header.Set("CF-Client-Version", "a-6.30-3596")
	resp, err := hc.Do(req)
	if err != nil {
		return Account{}, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return Account{}, fmt.Errorf("WARP registration: HTTP %d %s", resp.StatusCode, bytes.TrimSpace(data[:min(len(data), 200)]))
	}
	var r struct {
		ID     string `json:"id"`
		Token  string `json:"token"`
		Config struct {
			ClientID string `json:"client_id"`
			Peers    []struct {
				PublicKey string `json:"public_key"`
			} `json:"peers"`
			Interface struct {
				Addresses struct {
					V4 string `json:"v4"`
					V6 string `json:"v6"`
				} `json:"addresses"`
			} `json:"interface"`
		} `json:"config"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return Account{}, fmt.Errorf("WARP registration: %w", err)
	}
	if len(r.Config.Peers) == 0 || r.Config.Interface.Addresses.V4 == "" {
		return Account{}, fmt.Errorf("WARP registration: incomplete answer")
	}
	a := Account{ID: r.ID, Token: r.Token, PrivateKey: priv, PeerKey: r.Config.Peers[0].PublicKey,
		Address4: r.Config.Interface.Addresses.V4, Address6: r.Config.Interface.Addresses.V6, Created: time.Now().Unix()}
	if cid, err := base64.StdEncoding.DecodeString(r.Config.ClientID); err == nil && len(cid) == 3 {
		a.Reserved = []int{int(cid[0]), int(cid[1]), int(cid[2])}
	}
	return a, nil
}

// WARP's WireGuard servers answer on these ranges and ports. Networks that
// filter the usual 162.159.192.1:2408 often leave some other pair open.
var (
	prefixes = []string{"162.159.192.", "162.159.193.", "162.159.195.", "188.114.96.", "188.114.97.", "188.114.98.", "188.114.99."}
	ports    = []int{2408, 500, 1701, 4500, 854, 859, 864, 878, 880, 890, 891, 894, 903, 908, 928, 934, 939, 942,
		943, 945, 946, 955, 968, 987, 988, 1002, 1010, 1014, 1018, 1070, 1074, 1180, 1387, 1843, 2371, 2506,
		3138, 3476, 3581, 3854, 4177, 4198, 4233, 5279, 5956, 7103, 7152, 7156, 7281, 7559, 8319, 8742, 8854, 8886}
)

func randInt(n int) int {
	v, _ := rand.Int(rand.Reader, big.NewInt(int64(n)))
	return int(v.Int64())
}

// Candidates returns n endpoints: the default one first, the rest random.
func Candidates(n int) []string {
	out := []string{"162.159.192.1:2408"}
	seen := map[string]bool{out[0]: true}
	for len(out) < n {
		ep := fmt.Sprintf("%s%d:%d", prefixes[randInt(len(prefixes))], 1+randInt(254), ports[randInt(len(ports))])
		if !seen[ep] {
			seen[ep] = true
			out = append(out, ep)
		}
	}
	return out
}

// Endpoint is one sing-box WireGuard endpoint for the account.
func (a Account) Endpoint(tag, hostport string) map[string]any {
	host, port := hostport, 2408
	if i := strings.LastIndex(hostport, ":"); i > 0 {
		host = hostport[:i]
		fmt.Sscanf(hostport[i+1:], "%d", &port)
	}
	addrs := []any{a.Address4 + "/32"}
	if a.Address6 != "" {
		addrs = append(addrs, a.Address6+"/128")
	}
	peer := map[string]any{"address": host, "port": port, "public_key": a.PeerKey,
		"allowed_ips": []any{"0.0.0.0/0", "::/0"}, "persistent_keepalive_interval": 25}
	if len(a.Reserved) == 3 {
		peer["reserved"] = []any{a.Reserved[0], a.Reserved[1], a.Reserved[2]}
	}
	return map[string]any{"type": "wireguard", "tag": tag, "address": addrs, "private_key": a.PrivateKey,
		"mtu": 1280, "peers": []any{peer}}
}
