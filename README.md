<p align="center"><b>English</b> · <a href="README.fa.md">فارسی</a></p>

# FoxGozar

FoxGozar runs on your OpenWrt router and sends the internet traffic of every
device at home (phones, laptops, TVs, consoles) through your VPN servers. No app
is needed on the devices themselves: anything connected to the router's Wi-Fi
or cable is covered.

You manage it from the router's own web page (LuCI), in English or Persian,
with a light or dark theme.

## What it does

- **Never cuts your connection** when you add, remove or switch servers
- **Picks the fastest server by itself** and switches when one slows down or fails
- **Free WARP backup** that takes over when every server is down
- **Per-device control:** through the tunnel, direct, through one chosen server, or no internet
- **Iranian sites go direct** (faster, and they don't use your server's data)
- **Blocks ads, malware and phishing** for every device
- **Kill switch:** devices stay offline instead of going out unprotected when the tunnel drops
- **Real game ping** to the servers of Dota 2, CS2, League of Legends, Valorant and more
- **QR codes** to use the same servers on your phone outside home
- Works with share links (vless, vmess, trojan, ss, hy2, tuic …), subscriptions,
  Clash / Clash Meta configs and Xray / sing-box JSON configs

---

## Step-by-step installation, from zero

You don't need to know Linux. Follow the steps in order; each one says exactly
what to type.

### What you need

- A router with **OpenWrt 22.03 or newer**. (OpenWrt is the router's operating
  system. If your router still has its original software, install OpenWrt first:
  find your model on <https://firmware-selector.openwrt.org>.)
- About **130 MB of free space** on the router and **256 MB of memory** or more
- The router connected to the internet
- A Windows, macOS or Linux computer on the router's network
- At least one VPN server: a share link (starts with `vless://`, `vmess://`,
  `trojan://` …) or a subscription link from your provider

### Step 1: Open a terminal on your computer

- **Windows 10 / 11:** press the Start button, type `PowerShell`, and open it.
- **macOS:** open Launchpad, type `Terminal`, and open it.
- **Linux:** open the Terminal app.

A window opens where you type commands and press **Enter** after each one.

### Step 2: Connect to the router

Type:

```sh
ssh root@192.168.1.1
```

- If your router uses a different address, use that one instead of `192.168.1.1`.
- The first time, it asks `Are you sure you want to continue connecting?` Type `yes` and press Enter.
- Then it asks for a password: type the password you use for the router's web
  page. Nothing appears while you type; that is normal. Press Enter.

When you see a line ending in `root@OpenWrt:~#`, you are inside the router.

### Step 3: Find your router's version and architecture

Still inside the router, type:

```sh
cat /etc/openwrt_release
```

Look for two lines, for example:

```
DISTRIB_RELEASE='25.12.6'
DISTRIB_ARCH='arm_cortex-a7_neon-vfpv4'
```

Write both down:

- **Version** (`DISTRIB_RELEASE`): **25 or newer** → you need the **`.apk`** file.
  **24 or older** → you need the **`.ipk`** file.
- **Architecture** (`DISTRIB_ARCH`): the exact name, such as `arm_cortex-a7_neon-vfpv4`,
  `aarch64_cortex-a53` or `mipsel_24kc`.

### Step 4: Download the package

1. Open the [Releases](../../releases) page and open the newest version.
2. Under **Assets**, find the file whose name ends with **your architecture** and
   **your file type**. For example, for the router above:
   `foxgozar_6.13.3-r1_arm_cortex-a7_neon-vfpv4.apk`
3. Download it. It goes to your **Downloads** folder.

The architecture must match exactly: `arm_cortex-a7` and `arm_cortex-a7_neon-vfpv4`
are different files.

### Step 5: Copy the package to the router

Open a **second** terminal window (leave the first one connected to the router)
and type these two commands. Replace the file name with the one you downloaded:

```sh
cd Downloads
scp -O foxgozar_6.13.3-r1_arm_cortex-a7_neon-vfpv4.apk root@192.168.1.1:/tmp/
```

It asks for the router password again. When the progress line reaches 100 %,
the file is on the router.

> Prefer a window to drag files into? [WinSCP](https://winscp.net) works too:
> choose the **SCP** protocol, log in as `root`, and drop the file into `/tmp`.

### Step 6: Install it

Go back to the **first** terminal window (the one inside the router).

**OpenWrt 25 or newer (`.apk`):**

```sh
apk update
apk add --allow-untrusted /tmp/foxgozar_*.apk
```

**OpenWrt 24 or older (`.ipk`):**

```sh
opkg update
opkg install /tmp/foxgozar_*.ipk
```

The first command refreshes the router's package list; the second installs
FoxGozar and the two small system parts it needs. `--allow-untrusted` is
needed because the package is not from OpenWrt's own store.

### Step 7: Open FoxGozar

1. In your browser, go to `http://192.168.1.1` and log in.
2. From the top menu, open **Services → FoxGozar**.

If you don't see it, log out and back in, or press `Ctrl + F5` to reload the page.

### Step 8: Add your servers

**With share links:** open the **Servers** tab → **Add servers** → paste one or
more links (one per line) → **Add servers**.

**With a subscription:** open the **Subscriptions** tab → **Add subscription** →
paste the subscription link → **Add subscription**. A subscription keeps its servers up to date
by itself.

### Step 9: Connect

Press the big round button at the top.

The first time, it shows **Getting ready…** for a few minutes while the router
downloads the cores (sing-box, and Xray if your servers need it), about 100 MB.
After that, connecting takes under a second.

When it shows **Connected**, every device on the router is using the tunnel.
You can close the terminal windows.

### Step 10: Recommended settings

| Where | Setting | Why |
|---|---|---|
| Routing | **Iranian sites go direct** (on by default) | Faster, and saves your server's data |
| Routing | **Block ads** | Removes ads and trackers on every device |
| Routing | **Block malware and phishing** | Protection for the whole home |
| Settings | **Use WARP when every server fails** | Free backup so the internet keeps working |
| Settings | **Kill switch** | Nothing leaks out unprotected if the tunnel drops |

To choose how each device connects, open the **Devices** tab.

---

## Updating

Download the new package and repeat **steps 5 and 6**. Your servers and
settings are kept.

## Uninstalling

Inside the router (step 2):

```sh
apk del foxgozar          # OpenWrt 25 or newer
opkg remove foxgozar      # OpenWrt 24 or older
rm -rf /etc/foxgozar /usr/share/foxgozar
```

The last line also deletes your saved servers and the downloaded cores.

## Common problems

**`ssh` says "Connection refused" or times out.** Check that the computer is on
the router's Wi-Fi or cable, and that the address is right (it is printed on the
router or shown in your computer's network details as the *gateway*).

**`scp` fails with "subsystem request failed" or "sftp".** Add `-O` exactly as in
step 5.

**Installation says the architecture doesn't match.** You downloaded a file for
another architecture. Repeat steps 3 and 4.

**Installation can't download `kmod-nft-tproxy` or `ca-bundle`.** The router has
no internet, or `apk update` / `opkg update` was skipped. Fix the internet and run
step 6 again.

**It stays on "Getting ready…" or shows "Could not connect".** The router could
not download the cores from GitHub. Open **Settings → Advanced**, put a GitHub
mirror address in **GitHub mirror**, and press Connect again. The **Log**
tab shows the exact reason.

**Connected, but sites don't open.** Open the **Servers** tab and press **Test
all**. Remove servers marked failed with **Remove failed**, or add new ones.

**"The FoxGozar service is not answering."** It is starting; wait a few seconds.
If it stays, restart it inside the router: `/etc/init.d/foxgozar restart`.

---

## Building from source

Needs [Go](https://go.dev) (version in `go.mod`); no OpenWrt SDK.

```sh
go test ./...
go run ./tools/mkpkg -version 6.14.0 -out dist -arch arm_cortex-a7_neon-vfpv4   # one architecture
go run ./tools/mkpkg -version 6.14.0 -out dist                                  # all of them
```

## License

AGPL-3.0-or-later. Third-party notices are in [files/NOTICE](files/NOTICE).
