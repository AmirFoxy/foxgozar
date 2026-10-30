# FoxGozar

Fast transparent proxy for OpenWrt routers, managed from LuCI. sing-box is the
main core; Xray runs alongside it for VLESS ML-KEM encryption, XHTTP, mKCP and
WARP with UDP noise. The cores are downloaded on the router for its own
architecture, so a package holds only FoxGozar itself.

- Blue/green core restarts: changing servers or settings never cuts open connections
- Per-device policy (proxy / direct / one server / block), kill switch, WARP backup
- Subscriptions: share links, Clash YAML, sing-box and Xray JSON, SIP008
- Iranian routing rules (Chocolate4U), daily rule-set updates, ad and malware blocking
- Network graph, real game ping per server, QR codes for phones
- Persian and English, light and dark

## Install on a router

Find the router's architecture, then install the matching package from
[Releases](../../releases):

```sh
apk --print-arch                                 # OpenWrt 25.x (apk)
apk add --allow-untrusted foxgozar_*.apk

opkg print-architecture                          # OpenWrt 24.x and older (opkg)
opkg install foxgozar_*.ipk
```

Then open LuCI → Services → FoxGozar.

## Build

Needs Go (see `go.mod`). No OpenWrt SDK.

```sh
go test ./...
go run ./tools/mkpkg -version 6.14.0 -out dist -arch arm_cortex-a7_neon-vfpv4   # one architecture
go run ./tools/mkpkg -version 6.14.0 -out dist                                  # all of them
```

`mkpkg` refuses to package a UI script that does not parse.

## Release

```sh
git tag v6.14.0
git push origin master --tags
```

GitHub Actions (`.github/workflows/release.yml`) builds every architecture and
publishes the packages as a release.
