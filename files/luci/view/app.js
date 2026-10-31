'use strict';
'require view';

/*
 * FoxGozar web UI. Talks to the FoxGozar service through
 * /cgi-bin/foxgozar (same origin, so http and https both work), using the
 * LuCI session for auth.
 */

var RES = L.resource('foxgozar');
var BASE = (L.env.cgi_base || '/cgi-bin') + '/foxgozar';
var SID = L.env.sessionid;
var VERSION = '__FGZ_VERSION__'; // filled in by the packager

/* ------------------------------------------------------------ text */

var T = {
	en: {
		connected: 'Connected', disconnected: 'Not connected', starting: 'Connecting…',
		installing: 'Getting ready…', failed: 'Could not connect',
		viaWarp: 'Through WARP: every server failed, so the free backup is carrying traffic until one answers again',
		sWarp: 'WARP backup', warpBackup: 'Use WARP when every server fails', warpBackupHint: 'Free Cloudflare WARP takes over by itself and hands back as soon as a server answers. Plain WARP is filtered in Iran; FoxGozar sends it through Xray with noise and keeps several endpoints ready.',
		warpReady: '{0} of {1} WARP endpoints answer', warpNone: 'No WARP endpoint answers yet; new ones are tried by themselves', warpActive: 'WARP is carrying traffic now', warpWait: 'Registering and connecting to WARP… this takes a few seconds',
		via: 'Through {0}', idle: 'Your network uses its normal connection.',
		connect: 'Connect', disconnect: 'Disconnect',
		yourNet: 'Your network', exit: 'Exit',
		up: 'Up', down: 'Down', delay: 'Delay', used: 'Used', conns: 'Connections', upFor: 'Connected for',
		testServices: 'Test sites', testServicesHint: 'Checks which servers open YouTube, Instagram, Telegram, Google and X', share: 'Share', copy: 'Copy link', copied: 'Copied', close: 'Close',
		scanHint: 'Scan with v2rayNG, Hiddify, Streisand or V2Box to use this server outside home.', qrTooLong: 'This link is too long for a QR code; copy it instead.',
		testTarget: 'Test servers with', testTargetHint: 'Automatic choice only picks servers that open this site', custom: 'Other address',
		network: 'Network', games: 'Games', trafficTitle: 'Traffic, last two minutes', download: 'Download', upload: 'Upload', peak: 'peak',
		showTable: 'Show as table', hideTable: 'Hide table', when: 'When', secAgo: '{0} s ago', minsAgo: '{0} min ago', now: 'now',
		mapTitle: 'Your network', mapNote: 'Line width shows each device’s speed through the tunnel. Direct devices use the router’s own way out.',
		router: 'Router', internet: 'Internet', directPath: 'Direct, without the tunnel',
		testGames: 'Measure ping', retestGames: 'Measure again', gamesNote: 'Ping through each of your connections to the city where each game runs its servers, measured on an open connection the way a game plays. It lands within a few ms of in-game ping.',
		pingGreat: 'great', pingGood: 'good', pingOk: 'playable', pingHigh: 'high', notMeasured: 'not measured yet',
		noGames: 'Not measured yet', noGamesHint: 'Connect, then press Measure. It takes a few seconds.', bestFor: 'Best connection:', noneAnswered: 'No connection reached these servers',
		devices: 'Devices', newDevices: 'Devices without a setting of their own', viaTunnel: 'Through the tunnel', direct: 'Direct, without the tunnel', block: 'No internet', viaServer: 'Through {0}',
		noDevices: 'No devices seen yet', noDevicesHint: 'Devices appear here once they connect to the router.', offlineDev: 'offline', rename: 'Rename', devName: 'Device name',
		usageHint: 'Used through the tunnel since the router service started', directNotMeasured: 'direct traffic is not counted', server: 'Server',
		schedule: 'Offline hours', scheduleHint: 'Every day, no internet between these times. May cross midnight, for example 23:00 to 07:00.',
		scheduleOn: 'Offline hours on', from: 'From', to: 'To', offHours: 'no internet {0} to {1}', pausedNow: 'no internet now, until {0}',
		nodes: 'Servers', subs: 'Subscriptions', rules: 'Routing', settings: 'Settings', logs: 'Log',
		auto: 'Fastest, automatically', autoHint: 'Switches by itself when a server slows down or fails',
		autoSub: 'Fastest of this subscription', autoSubHint: 'Picks automatically, but only among this subscription’s servers',
		addedByYou: 'Added by you', add: 'Add servers', testAll: 'Test all', removeFailed: 'Remove failed',
		search: 'Search servers', test: 'Test', remove: 'Remove', failedShort: 'failed',
		noNodes: 'No servers yet', noNodesHint: 'Add share links (vless://, vmess://, trojan://, ss://, hy2:// …) or a subscription.',
		addTitle: 'Add servers', addHint: 'Paste share links, a subscription’s content, a Clash or Clash Meta config (YAML), or an Xray or sing-box JSON config.',
		cancel: 'Cancel', added: 'Added {0} servers', skipped: '{0} lines could not be read',
		testing: 'Testing…', tested: '{0} of {1} servers answered', removed: 'Removed {0} servers',
		connectFirst: 'Connect first: servers are tested through the tunnel.',
		addSub: 'Add subscription', subUrl: 'Subscription link', subName: 'Name (optional)',
		update: 'Update', updated: 'Updated', noSubs: 'No subscriptions', noSubsHint: 'A subscription keeps its servers up to date by itself.',
		servers: '{0} servers', expires: 'Expires in {0} days', expired: 'Expired', lastUpdate: 'Updated {0}',
		every: 'Every {0} hours', of: '{0} of {1}',
		subExpired: 'Subscription “{0}” has expired', subExpiresSoon: 'Subscription “{0}” expires in {1} days', subAlmostUsed: 'Subscription “{0}”: {1}% of its traffic used',
		iranDirect: 'Iranian sites go direct', iranDirectHint: 'Faster, and does not use your server’s traffic',
		blockAds: 'Block ads', blockAdsHint: 'Ad and tracker domains, Persian and foreign, for every device. Ads inside YouTube videos come from the same servers as the videos, so no router can block them; use uBlock Origin in the browser, or ReVanced or SmartTube on Android.',
		blockMalware: 'Block malware and phishing', blockMalwareHint: 'Known malware, phishing and crypto-mining addresses',
		quicBlock: 'Block QUIC', quicBlockHint: 'Makes video and browsers use TCP; helps on some servers',
		customRules: 'Your rules', addRule: 'Add rule', rulePlaceholder: 'Domains, IP ranges, keyword:word or a list link',
		rulesHelp: 'Separate entries with commas. example.com also covers its subdomains; keyword:google matches any domain containing google; a link (https://…) to a sing-box rule-set or a plain list of domains is downloaded and refreshed daily.',
		toDirect: 'Direct', toProxy: 'Through the tunnel', toBlock: 'Block', save: 'Save', saved: 'Saved',
		noRules: 'No rules yet. Example: openai.com through a specific server.',
		sConnection: 'Connection', sDns: 'DNS', sNetwork: 'Network', sAdvanced: 'Advanced', sCores: 'Cores',
		testUrl: 'Test address', testUrlHint: 'Used to measure every server',
		testInterval: 'Test every (seconds)', tolerance: 'Switch when faster by (ms)', toleranceHint: 'Avoids hopping between servers that are nearly as fast',
		remoteDns: 'DNS through the tunnel', directDns: 'DNS for direct sites', directDnsHint: 'isp = your internet provider’s DNS (best for Iranian sites), or an address such as udp://8.8.8.8', fakeIp: 'Fake IP', fakeIpHint: 'Faster first connections; some apps dislike it',
		proxyRouter: 'Router’s own traffic through the tunnel', ipv6: 'IPv6', killSwitch: 'Kill switch', killSwitchHint: 'Devices stay offline while the tunnel is down, instead of going out unprotected',
		lan: 'LAN interfaces', lanHint: 'Comma-separated, usually br-lan',
		mux: 'Multiplex', muxHint: 'Fewer handshakes on servers that support it',
		fragment: 'TLS fragment', fragmentHint: 'Splits the TLS hello; can get past some filtering',
		autoRescue: 'Try again when filtering changes', autoRescueHint: 'When every server stops answering, FoxGozar tries TLS fragment and multiplex on and off by itself, keeps what works, and says so in the log',
		keepWarm: 'Keep core ready', keepWarmHint: 'Connect takes under a second; uses about 40 MB of memory',
		logLevel: 'Log detail', mirror: 'GitHub mirror', mirrorHint: 'Used when GitHub is unreachable',
		coreBeta: 'Use beta cores',
		thisMonth: 'this month {0}', cMonth: 'Servers this month', monthHint: 'Traffic through your servers this calendar month, kept across restarts. A close estimate, not your provider’s meter.',
		doctor: 'Site doctor', doctorHint: 'A site does not open? Type it: FoxGozar tries it directly and through every server, says what is wrong, and makes the rule that fixes it.', doctorPh: 'Site, e.g. example.com', doctorGo: 'Check',
		drOpens: 'opens', drChallenge: 'answers (a robot check: fine in a browser)', drSanctioned: 'this service refuses Iran', drFilteredDns: 'filtered (Iran’s filter answered)', drFilteredSni: 'filtered (the connection is cut)',
		drCountry: 'the site refuses this country', drRefused: 'the site refused ({0})', drTimeout: 'no answer', drTls: 'secure connection failed', drDns: 'name not found', drError: 'error',
		drWhyDirect: '{0} opens without the tunnel: going direct is faster and spares your server’s traffic.', drWhyTunnel: '{0} opens only through the tunnel.', drWhySome: '{0} opens through {1} only.', drWhyNothing: '{0} opens no way: it may be down, or refuse every address tried.',
		drWhySanctioned: 'It refuses users in Iran even where its front page opens, so it stays on the tunnel.', drApply: 'Send it {0}', drApplied: '{0} now goes {1}', drAlready: 'Your rules already do this.',
		uptimeHint: 'The last 24 hours, half an hour a bar: green answered, red did not, yellow partly, grey not measured. The percentage is over the last week.', uptimePct: '{0}% up this week', uptimeNone: 'not measured yet',
		smartAuto: 'Skip unsteady servers', smartAutoHint: 'Every half hour each server opens a few tiny connections. One whose connections come up unsteadily is left out of the automatic choice while a steady one is left (still selectable by hand).',
		autoDirect: 'Games go direct by themselves when better', autoDirectHint: 'After every match FoxGozar checks whether that game’s own servers answer directly and faster than the tunnel, and shows it on its card. With this on, such a game plays direct from its next match; its launcher stays on the tunnel. Never changes during a match.',
		moreCities: '+{0} more cities', notRoutable: 'This game’s match traffic cannot be told apart from the rest, so it follows the tunnel.',
		searchGamesPh: 'Search a game to add…', searching: 'Searching…', noGameFound: 'Nothing found', inList: 'on the list', restoreGame: 'bring back', addThis: 'add',
		removeGame: 'Remove from the list', removeGameAsk: 'Press again to remove {0}',
		checkDirect: 'Check game server', checkDirectHint: 'Pings the servers this game was played on, outside the tunnel: is the game itself reachable directly, and faster?',
		playFirst: 'Play one match first: FoxGozar learns the game’s servers from it.', noPingAnswer: 'The game’s servers do not answer pings, so this cannot be told. You can still try direct.',
		directBetter: 'The game server is reachable directly: {0} ms, through the tunnel {1} ms. The launcher stays on the tunnel.', tunnelBetter: 'Reachable directly ({0} ms), but the tunnel is faster ({1} ms).', useDirect: 'Play direct',
		gameDl: 'Game downloads go direct', gameDlHint: 'Steam, Epic, Battle.net and Riot downloads and updates skip the tunnel: they are huge, and this spares your server’s traffic and speed. Logins and the launchers themselves stay on the tunnel.',
		chooseGames: 'Choose games', chooseGamesHint: 'Tick the games to show. To add one, type its name: FoxGozar finds it in Steam’s store and fetches its picture.', addGame: 'Add a game', addGamePh: 'Game name, e.g. Rust', noGamesShown: 'No games shown. Choose some with “Choose games”.',
		gameAdded: '{0} added', gameAddedNoArt: '{0} added; Steam does not have it, so it has no picture', customGameHint: 'Its server cities are not known, so every measured city is shown: play on the one with the best ping.',
		unreachable: 'unreachable', jitterHint: 'How much the ping wobbles from one moment to the next', p90Hint: 'Nine in ten moments stay under this', spikesTo: 'spikes to {0}',
		antiLag: 'Anti-lag', antiLagLabel: 'Keep ping low while others download', antiLagHint: 'Holds traffic on the router just below your line’s speed (OpenWrt’s SQM with cake), so game and call packets do not wait behind downloads. The first time it installs two official OpenWrt packages.',
		lineSpeed: 'Your line’s speed (Mbit/s)', lineSpeedHint: 'Measure with the speedtest.net app with every VPN off, and enter the numbers as they are',
		lagInstalling: 'Installing SQM…', lagActive: 'On: cake is shaping the internet port', lagStarting: 'Starting…',
		directPath: 'Direct (no tunnel)', gamePath: 'This game’s traffic goes', gameViaTunnel: 'Through the tunnel, like the rest', gameBest: 'Fastest in the last test: {0}, {1} ms',
		directGameHint: 'Direct works only if the game does not block players from Iran; if it will not connect, choose a server.',
		speedTest: 'Speed test', speedAll: 'Test speed', speedAllHint: 'Download speed and connection steadiness of every server, one by one. Uses up to 25 MB of each server’s traffic.', speedTesting: 'Testing {0} of {1}…', speedDone: 'Speed test done',
		speedHint: 'Download speed through this server', speedFailed: 'no download', unstable: 'unsteady',
		unstableHint: '{0} of {1} new connections were slow or failed (best {2} ms): pages and apps sometimes hang for seconds. Usually the server’s network or filtering, not this router.',
		quick: 'Quick settings', qServer: 'Server', qAuto: 'Automatic', qManual: 'Manual', qIran: 'Iranian sites', qDirect: 'Direct', qTunnel: 'Tunnel', on: 'On', off: 'Off',
		cConn: 'Connected for', cMemory: 'Core memory', cBest: 'Best server', cAvg: 'Average', cTraffic: 'Traffic', cNow: 'Now', cTotal: 'Total',
		cSub: 'Subscription', cQuota: 'Used', cServers: 'Servers', cWorking: 'Answering',
		sSharing: 'Share this router’s internet', sharing: 'Sharing server',
		sharingHint: 'For a shutdown: with Starlink (or another way out) on the router, people on the domestic network connect here and use it. Each person gets an account of their own below.',
		newKeyDone: 'New server key made: every account’s link changed, send them again',
		share: 'Sharing', sharingNoUsers: 'On, but no account yet: add one below', sharingOff: 'Not running',
		newKeyHint: 'Changes every account’s link at once', accounts: 'Accounts ({0})', addAccount: 'New account',
		accountsHint: 'One account per person, so you see who uses how much and can pause or remove one alone. Give the link in person (QR) or by SMS; messengers inside Iran are watched. During a shutdown app stores do not work: people need v2rayNG, Hiddify or Streisand installed beforehand, or the app file from you.',
		noAccounts: 'No accounts yet', noAccountsHint: 'Add one for each person who should use this router’s internet.',
		onlineNow: 'connected now ({0})', lastSeen: 'last used {0}', neverSeen: 'not used yet',
		pausedLimit: 'Paused: data limit reached', pausedByYou: 'Paused', accountOn: 'Account on',
		removeAccount: 'Press again to remove {0}’s account', accountName: 'Who is it for', accountNamePh: 'Name, e.g. Sara',
		limitGB: 'Data limit (GB)', noLimit: 'No limit', resetUsage: 'Reset the usage count', newLink: 'New link (the old one stops working)',
		addAccountHint: 'A link of its own is made for this person; you can send it right after.', newLinkDone: 'New link made: send it again',
		copyMessage: 'Copy message',
		howToUse: 'This link is for you only; do not pass it on.\nAndroid: install v2rayNG or Hiddify, copy the link below, choose “Import from clipboard” in the app, then connect.\niPhone: install Streisand or V2Box and do the same.\nIf it does not connect, tell me.',
		sharingPort: 'Port', sharingPortHint: 'If the domestic modem is a router, forward this TCP port to this router',
		sharingSni: 'Looks like a visit to', sharingSniHint: 'A busy site with TLS 1.3. Probes at the port see that site.',
		sharingOn: 'Running', sharingDown: 'Not running yet; see the log', sharingAt: 'People connect to {0} (on {1})',
		sharingNoAddr: 'The router has no internet address yet', showLink: 'Link and QR', newKey: 'New key',
		dualwanOn: 'Two internet ports are on: the second port (wan2) is the domestic line, Iranian addresses go over it, everything else over the first. To undo, over SSH:',
		dualwanOff: 'To use the router’s second port for a domestic line while the first has Starlink, run this over SSH. The wired LAN port stops being LAN; Wi-Fi stays.',
		sBackup: 'Backup', backup: 'Back up everything', backupHint: 'Servers, subscriptions, rules, devices and settings in one file. Keep it private: it holds your server keys.',
		download: 'Download', restore: 'Restore from a file', restoreHint: 'Replaces everything here with the file’s contents',
		chooseFile: 'Choose file', notBackup: 'Not a FoxGozar backup file', restored: 'Restored: {0} servers, {1} subscriptions, {2} devices',
		installed: 'Installed: {0}', notInstalled: 'Not installed', updateCore: 'Update {0}', updateRules: 'Update rule-sets',
		updating: 'Updating…', theme: 'Theme', lang: 'فارسی',
		offline: 'The FoxGozar service is not answering. It may be starting; this page retries by itself.',
		justNow: 'just now', minAgo: '{0} min ago', hAgo: '{0} h ago', dAgo: '{0} d ago'
	},
	fa: {
		connected: 'وصل است', disconnected: 'وصل نیست', starting: 'در حال اتصال…',
		installing: 'در حال آماده‌سازی…', failed: 'اتصال برقرار نشد',
		viaWarp: 'از طریق WARP: همه سرورها قطع‌اند و پشتیبان رایگان ترافیک را می‌برد تا یکی دوباره جواب بدهد',
		sWarp: 'پشتیبان WARP', warpBackup: 'وقتی همه سرورها قطع شدند از WARP استفاده کن', warpBackupHint: 'WARP رایگان کلادفلر خودش جایگزین می‌شود و به محض جواب دادن یک سرور کنار می‌رود. WARP ساده در ایران فیلتر است؛ فاکس‌گذر آن را با نویز از طریق Xray می‌فرستد و چند آدرس را آماده نگه می‌دارد.',
		warpReady: '{0} از {1} آدرس WARP جواب می‌دهند', warpNone: 'هنوز هیچ آدرس WARP جواب نمی‌دهد؛ آدرس‌های تازه خودکار امتحان می‌شوند', warpActive: 'الان WARP ترافیک را می‌برد', warpWait: 'در حال ثبت‌نام و اتصال به WARP… چند ثانیه طول می‌کشد',
		via: 'از طریق {0}', idle: 'شبکه شما از اینترنت عادی استفاده می‌کند.',
		connect: 'اتصال', disconnect: 'قطع اتصال',
		yourNet: 'شبکه شما', exit: 'خروجی',
		up: 'ارسال', down: 'دریافت', delay: 'پینگ', used: 'مصرف', conns: 'اتصال‌ها', upFor: 'مدت اتصال',
		testServices: 'تست سایت‌ها', testServicesHint: 'بررسی می‌کند کدام سرورها یوتیوب، اینستاگرام، تلگرام، گوگل و X را باز می‌کنند', share: 'اشتراک‌گذاری', copy: 'کپی لینک', copied: 'کپی شد', close: 'بستن',
		scanHint: 'با v2rayNG، Hiddify، Streisand یا V2Box اسکن کنید تا بیرون از خانه هم از همین سرور استفاده کنید.', qrTooLong: 'این لینک برای QR کد خیلی طولانی است؛ آن را کپی کنید.',
		testTarget: 'تست سرورها با', testTargetHint: 'انتخاب خودکار فقط سرورهایی را برمی‌دارد که این سایت را باز می‌کنند', custom: 'آدرس دیگر',
		network: 'شبکه', games: 'بازی‌ها', trafficTitle: 'ترافیک دو دقیقه اخیر', download: 'دریافت', upload: 'ارسال', peak: 'بیشینه',
		showTable: 'نمایش جدول', hideTable: 'بستن جدول', when: 'زمان', secAgo: '{0} ثانیه پیش', minsAgo: '{0} دقیقه پیش', now: 'الان',
		mapTitle: 'شبکه شما', mapNote: 'ضخامت هر خط سرعت آن دستگاه از تونل را نشان می‌دهد. دستگاه‌های مستقیم از راه خود روتر بیرون می‌روند.',
		router: 'روتر', internet: 'اینترنت', directPath: 'مستقیم، بدون تونل',
		testGames: 'اندازه‌گیری پینگ', retestGames: 'اندازه‌گیری دوباره', gamesNote: 'پینگ هر کدام از اتصال‌های شما تا شهری که سرورهای هر بازی در آن است، روی اتصال باز و همان‌طور که بازی کار می‌کند. چند میلی‌ثانیه با پینگ داخل بازی فاصله دارد.',
		pingGreat: 'عالی', pingGood: 'خوب', pingOk: 'قابل بازی', pingHigh: 'بالا', notMeasured: 'هنوز اندازه‌گیری نشده',
		noGames: 'هنوز اندازه‌گیری نشده', noGamesHint: 'وصل شوید و «اندازه‌گیری» را بزنید. چند ثانیه طول می‌کشد.', bestFor: 'بهترین اتصال:', noneAnswered: 'هیچ اتصالی به این سرورها نرسید',
		devices: 'دستگاه‌ها', newDevices: 'دستگاه‌هایی که تنظیم جداگانه ندارند', viaTunnel: 'از تونل', direct: 'مستقیم، بدون تونل', block: 'بدون اینترنت', viaServer: 'از طریق {0}',
		noDevices: 'هنوز دستگاهی دیده نشده', noDevicesHint: 'دستگاه‌ها بعد از وصل شدن به روتر این‌جا نمایش داده می‌شوند.', offlineDev: 'آفلاین', rename: 'تغییر نام', devName: 'نام دستگاه',
		usageHint: 'مصرف از تونل از زمان شروع سرویس روتر', directNotMeasured: 'ترافیک مستقیم شمرده نمی‌شود', server: 'سرور',
		schedule: 'ساعت‌های بدون اینترنت', scheduleHint: 'هر روز، بین این دو ساعت اینترنت قطع است. می‌تواند از نیمه‌شب رد شود، مثلاً ۲۳:۰۰ تا ۰۷:۰۰.',
		scheduleOn: 'ساعت‌های بدون اینترنت روشن', from: 'از', to: 'تا', offHours: 'بدون اینترنت از {0} تا {1}', pausedNow: 'الان بدون اینترنت، تا {0}',
		nodes: 'سرورها', subs: 'اشتراک‌ها', rules: 'مسیریابی', settings: 'تنظیمات', logs: 'گزارش',
		auto: 'سریع‌ترین، خودکار', autoHint: 'وقتی سروری کند یا قطع شود، خودش عوض می‌کند',
		autoSub: 'سریع‌ترین همین اشتراک', autoSubHint: 'خودکار انتخاب می‌کند، ولی فقط بین سرورهای همین اشتراک',
		addedByYou: 'افزوده‌شده توسط شما', add: 'افزودن سرور', testAll: 'تست همه', removeFailed: 'حذف خراب‌ها',
		search: 'جستجوی سرور', test: 'تست', remove: 'حذف', failedShort: 'خراب',
		noNodes: 'هنوز سروری ندارید', noNodesHint: 'لینک کانفیگ (vless://، vmess://، trojan://، ss://، hy2:// …) یا یک اشتراک اضافه کنید.',
		addTitle: 'افزودن سرور', addHint: 'لینک کانفیگ، محتوای یک اشتراک، کانفیگ Clash یا Clash Meta (YAML)، یا کانفیگ JSON ایکس‌ری یا سینگ‌باکس را بچسبانید.',
		cancel: 'انصراف', added: '{0} سرور اضافه شد', skipped: '{0} خط خوانده نشد',
		testing: 'در حال تست…', tested: '{0} از {1} سرور جواب دادند', removed: '{0} سرور حذف شد',
		connectFirst: 'اول وصل شوید: تست سرورها از داخل تونل انجام می‌شود.',
		addSub: 'افزودن اشتراک', subUrl: 'لینک اشتراک', subName: 'نام (اختیاری)',
		update: 'به‌روزرسانی', updated: 'به‌روز شد', noSubs: 'اشتراکی ندارید', noSubsHint: 'اشتراک، سرورهایش را خودش به‌روز نگه می‌دارد.',
		servers: '{0} سرور', expires: '{0} روز مانده', expired: 'منقضی شده', lastUpdate: 'به‌روزرسانی {0}',
		every: 'هر {0} ساعت', of: '{0} از {1}',
		subExpired: 'اشتراک «{0}» منقضی شده', subExpiresSoon: 'اشتراک «{0}» {1} روز دیگر تمام می‌شود', subAlmostUsed: 'اشتراک «{0}»: {1}٪ حجم مصرف شده',
		iranDirect: 'سایت‌های ایرانی مستقیم', iranDirectHint: 'سریع‌تر است و از حجم سرور شما کم نمی‌کند',
		blockAds: 'مسدودسازی تبلیغات', blockAdsHint: 'دامنه‌های تبلیغاتی و ردیاب ایرانی و خارجی، برای همه دستگاه‌ها. تبلیغ‌های داخل ویدیوهای یوتیوب از همان سرورهای خود ویدیو می‌آیند و هیچ روتری نمی‌تواند جلویشان را بگیرد؛ برای آن‌ها در مرورگر uBlock Origin و در اندروید ReVanced یا SmartTube را استفاده کنید.',
		blockMalware: 'مسدودسازی بدافزار و فیشینگ', blockMalwareHint: 'آدرس‌های شناخته‌شده بدافزار، فیشینگ و استخراج رمزارز',
		quicBlock: 'بستن QUIC', quicBlockHint: 'ویدیو و مرورگر از TCP استفاده می‌کنند؛ روی بعضی سرورها بهتر است',
		customRules: 'قوانین شما', addRule: 'افزودن قانون', rulePlaceholder: 'دامنه، رنج IP، keyword:کلمه یا لینک لیست',
		rulesHelp: 'موارد را با کاما جدا کنید. example.com زیردامنه‌هایش را هم شامل می‌شود؛ keyword:google هر دامنه‌ای که google در آن باشد؛ لینک (https://…) به rule-set سینگ‌باکس یا یک لیست ساده دامنه دانلود می‌شود و هر روز به‌روز می‌شود.',
		toDirect: 'مستقیم', toProxy: 'از تونل', toBlock: 'مسدود', save: 'ذخیره', saved: 'ذخیره شد',
		noRules: 'هنوز قانونی ندارید. مثلاً openai.com از یک سرور مشخص.',
		sConnection: 'اتصال', sDns: 'DNS', sNetwork: 'شبکه', sAdvanced: 'پیشرفته', sCores: 'هسته‌ها',
		testUrl: 'آدرس تست', testUrlHint: 'برای اندازه‌گیری همه سرورها',
		testInterval: 'تست هر چند ثانیه', tolerance: 'عوض کن وقتی این‌قدر سریع‌تر است (ms)', toleranceHint: 'از پریدن بین سرورهای تقریباً هم‌سرعت جلوگیری می‌کند',
		remoteDns: 'DNS از داخل تونل', directDns: 'DNS سایت‌های مستقیم', directDnsHint: 'isp یعنی DNS اپراتور اینترنت شما (برای سایت‌های ایرانی بهترین است)، یا یک آدرس مثل udp://8.8.8.8', fakeIp: 'Fake IP', fakeIpHint: 'اتصال اول سریع‌تر؛ بعضی برنامه‌ها با آن مشکل دارند',
		proxyRouter: 'ترافیک خود روتر از تونل', ipv6: 'IPv6', killSwitch: 'کیل‌سوییچ', killSwitchHint: 'وقتی تونل قطع است دستگاه‌ها آفلاین می‌مانند، نه بدون محافظت',
		lan: 'اینترفیس‌های LAN', lanHint: 'با کاما جدا کنید، معمولاً br-lan',
		mux: 'Multiplex', muxHint: 'روی سرورهایی که پشتیبانی می‌کنند، هندشیک کمتر',
		fragment: 'TLS fragment', fragmentHint: 'Hello تی‌ال‌اس را تکه می‌کند؛ گاهی از فیلتر رد می‌شود',
		autoRescue: 'تلاش دوباره وقتی فیلترینگ عوض می‌شود', autoRescueHint: 'وقتی هیچ سروری جواب نمی‌دهد، فاکس‌گذر خودش TLS fragment و multiplex را روشن و خاموش امتحان می‌کند، هر کدام کار کرد نگه می‌دارد و در گزارش می‌نویسد',
		keepWarm: 'هسته آماده بماند', keepWarmHint: 'اتصال زیر یک ثانیه؛ حدود ۴۰ مگابایت رم',
		logLevel: 'جزئیات گزارش', mirror: 'میرور GitHub', mirrorHint: 'وقتی GitHub در دسترس نیست',
		coreBeta: 'هسته‌های بتا',
		thisMonth: 'این ماه {0}', cMonth: 'مصرف سرورها این ماه', monthHint: 'ترافیک سرورهایتان در این ماه، که با ری‌استارت پاک نمی‌شود. تخمینی نزدیک است، نه شمارنده خود سرور.',
		doctor: 'دکتر سایت', doctorHint: 'سایتی باز نمی‌شود؟ آدرسش را بنویس: فاکس‌گذر آن را مستقیم و از هر سرور امتحان می‌کند، می‌گوید مشکل چیست و قانونی که درستش می‌کند را می‌سازد.', doctorPh: 'آدرس سایت، مثلاً example.com', doctorGo: 'بررسی',
		drOpens: 'باز می‌شود', drChallenge: 'جواب می‌دهد (بررسی ربات؛ در مرورگر مشکلی نیست)', drSanctioned: 'این سرویس ایران را تحریم کرده', drFilteredDns: 'فیلتر (فیلترینگ ایران جواب داد)', drFilteredSni: 'فیلتر (اتصال قطع می‌شود)',
		drCountry: 'سایت این کشور را نمی‌پذیرد', drRefused: 'سایت رد کرد ({0})', drTimeout: 'جواب نداد', drTls: 'اتصال امن برقرار نشد', drDns: 'آدرس پیدا نشد', drError: 'خطا',
		drWhyDirect: '{0} بدون تونل باز می‌شود: مستقیم سریع‌تر است و حجم سرورت را مصرف نمی‌کند.', drWhyTunnel: '{0} فقط از تونل باز می‌شود.', drWhySome: '{0} فقط از {1} باز می‌شود.', drWhyNothing: '{0} از هیچ راهی باز نمی‌شود: شاید از کار افتاده، یا همه آدرس‌ها را رد می‌کند.',
		drWhySanctioned: 'این سرویس کاربران ایران را رد می‌کند، حتی اگر صفحه اولش باز شود؛ پس از تونل می‌ماند.', drApply: 'بفرستش {0}', drApplied: '{0} از این به بعد {1}', drAlready: 'قوانین تو همین کار را می‌کنند.',
		uptimeHint: '۲۴ ساعت اخیر، هر نوار نیم ساعت: سبز جواب داده، قرمز نداده، زرد گاهی، خاکستری اندازه‌گیری نشده. درصد مربوط به هفته اخیر است.', uptimePct: '{0}٪ در دسترس این هفته', uptimeNone: 'هنوز اندازه‌گیری نشده',
		smartAuto: 'رد کردن سرورهای ناپایدار', smartAutoHint: 'هر نیم ساعت از هر سرور چند اتصال کوچک باز می‌شود. سروری که اتصال‌هایش ناپایدار باز می‌شوند از انتخاب خودکار کنار گذاشته می‌شود، به شرط اینکه سرور پایداری باقی بماند (دستی هنوز قابل انتخاب است).',
		autoDirect: 'بازی‌ها خودکار مستقیم شوند وقتی بهتر است', autoDirectHint: 'بعد از هر بازی، فاکس‌گذر بررسی می‌کند که سرورهای خود آن بازی مستقیم و سریع‌تر از تونل جواب می‌دهند یا نه، و روی کارتش نشان می‌دهد. با روشن بودن این گزینه، آن بازی از بازی بعد مستقیم می‌رود؛ لانچرش از تونل می‌ماند. وسط بازی هیچ‌وقت عوض نمی‌شود.',
		moreCities: '+{0} شهر دیگر', notRoutable: 'ترافیک بازی این بازی از بقیه قابل تشخیص نیست، پس از تونل می‌رود.',
		searchGamesPh: 'جستجوی بازی برای افزودن…', searching: 'در حال جستجو…', noGameFound: 'چیزی پیدا نشد', inList: 'در لیست است', restoreGame: 'برگرداندن', addThis: 'افزودن',
		removeGame: 'حذف از لیست', removeGameAsk: 'برای حذف {0} دوباره بزنید',
		checkDirect: 'بررسی سرور بازی', checkDirectHint: 'سرورهایی را که این بازی رویشان اجرا شده، بیرون از تونل پینگ می‌کند: آیا خود بازی مستقیم در دسترس است و سریع‌تر؟',
		playFirst: 'اول یک بازی کامل بکن: فاکس‌گذر سرورهای بازی را از همان یاد می‌گیرد.', noPingAnswer: 'سرورهای این بازی به پینگ جواب نمی‌دهند، پس نمی‌شود فهمید. می‌توانی مستقیم را امتحان کنی.',
		directBetter: 'سرور بازی مستقیم در دسترس است: {0} میلی‌ثانیه، از تونل {1}. لانچر از تونل می‌ماند.', tunnelBetter: 'مستقیم در دسترس است ({0} میلی‌ثانیه)، ولی تونل سریع‌تر است ({1}).', useDirect: 'بازی مستقیم',
		gameDl: 'دانلود بازی‌ها مستقیم', gameDlHint: 'دانلود و آپدیت Steam، Epic، Battle.net و Riot از تونل رد نمی‌شوند: حجمشان زیاد است و این‌طوری حجم و سرعت سرورتان حفظ می‌شود. لاگین و خود لانچرها از تونل می‌روند.',
		chooseGames: 'انتخاب بازی‌ها', chooseGamesHint: 'بازی‌هایی را که می‌خواهید نشان داده شوند تیک بزنید. برای افزودن، اسمش را بنویسید: فاکس‌گذر آن را در فروشگاه Steam پیدا می‌کند و تصویرش را می‌آورد.', addGame: 'افزودن بازی', addGamePh: 'اسم بازی، مثلاً Rust', noGamesShown: 'هیچ بازی‌ای نشان داده نمی‌شود. از «انتخاب بازی‌ها» انتخاب کنید.',
		gameAdded: '{0} اضافه شد', gameAddedNoArt: '{0} اضافه شد؛ Steam آن را ندارد، پس تصویری ندارد', customGameHint: 'شهر سرورهای این بازی معلوم نیست، پس همه شهرهای اندازه‌گیری‌شده نشان داده می‌شوند: روی شهری بازی کنید که بهترین پینگ را دارد.',
		unreachable: 'در دسترس نیست', jitterHint: 'پینگ از یک لحظه به لحظه بعد چقدر بالا و پایین می‌رود', p90Hint: 'نه از ده لحظه زیر این عدد است', spikesTo: 'پرش تا {0}',
		antiLag: 'ضد لگ', antiLagLabel: 'پینگ پایین بماند وقتی بقیه دانلود می‌کنند', antiLagHint: 'ترافیک را روی روتر کمی پایین‌تر از سرعت خط نگه می‌دارد (SQM با cake در OpenWrt)، تا بسته‌های بازی و تماس پشت دانلودها منتظر نمانند. بار اول دو بسته رسمی OpenWrt را نصب می‌کند.',
		lineSpeed: 'سرعت خط شما (مگابیت)', lineSpeedHint: 'با اپ speedtest.net و با همه VPNها خاموش اندازه بگیرید و همان اعداد را بنویسید',
		lagInstalling: 'در حال نصب SQM…', lagActive: 'روشن: cake پورت اینترنت را مدیریت می‌کند', lagStarting: 'در حال شروع…',
		directPath: 'مستقیم (بدون تونل)', gamePath: 'ترافیک این بازی از', gameViaTunnel: 'از تونل، مثل بقیه', gameBest: 'سریع‌ترین در آخرین تست: {0}، {1} میلی‌ثانیه',
		directGameHint: 'مستقیم فقط وقتی کار می‌کند که بازی بازیکنان ایران را نبندد؛ اگر وصل نشد، یک سرور انتخاب کنید.',
		speedTest: 'تست سرعت', speedAll: 'تست سرعت', speedAllHint: 'سرعت دانلود و پایداری اتصال همه سرورها، یکی‌یکی. از هر سرور تا ۲۵ مگابایت حجم مصرف می‌کند.', speedTesting: 'تست {0} از {1}…', speedDone: 'تست سرعت تمام شد',
		speedHint: 'سرعت دانلود از این سرور', speedFailed: 'دانلود نشد', unstable: 'ناپایدار',
		unstableHint: '{0} از {1} اتصال جدید کند بودند یا وصل نشدند (بهترین {2} میلی‌ثانیه): صفحه‌ها و برنامه‌ها گاهی چند ثانیه گیر می‌کنند. معمولاً از شبکه سرور یا فیلترینگ است، نه این روتر.',
		quick: 'دسترسی سریع', qServer: 'سرور', qAuto: 'خودکار', qManual: 'دستی', qIran: 'سایت‌های ایرانی', qDirect: 'مستقیم', qTunnel: 'از تونل', on: 'روشن', off: 'خاموش',
		cConn: 'مدت اتصال', cMemory: 'رم هسته', cBest: 'بهترین سرور', cAvg: 'میانگین', cTraffic: 'ترافیک', cNow: 'الان', cTotal: 'کل',
		cSub: 'اشتراک', cQuota: 'مصرف', cServers: 'سرورها', cWorking: 'جواب می‌دهند',
		sSharing: 'اشتراک اینترنت این روتر', sharing: 'سرور اشتراک‌گذاری',
		sharingHint: 'برای قطعی اینترنت: وقتی روتر استارلینک (یا راه دیگری به بیرون) دارد، بقیه از طریق اینترنت داخلی به اینجا وصل می‌شوند و از آن استفاده می‌کنند. هر نفر پایین یک حساب جدا می‌گیرد.',
		newKeyDone: 'کلید جدید سرور ساخته شد: لینک همه حساب‌ها عوض شد، دوباره بفرستید',
		share: 'اشتراک‌گذاری', sharingNoUsers: 'روشن است ولی هنوز حسابی ندارد: پایین یکی بسازید', sharingOff: 'اجرا نمی‌شود',
		newKeyHint: 'لینک همه حساب‌ها را یک‌جا عوض می‌کند', accounts: 'حساب‌ها ({0})', addAccount: 'حساب جدید',
		accountsHint: 'برای هر نفر یک حساب، تا ببینید چه کسی چقدر مصرف می‌کند و بتوانید هر کدام را جدا متوقف یا حذف کنید. لینک را حضوری (QR) یا با پیامک بدهید؛ پیام‌رسان‌های داخلی زیر نظرند. در قطعی، فروشگاه‌های برنامه کار نمی‌کنند: هر کس باید v2rayNG، Hiddify یا Streisand را از قبل نصب کرده باشد، یا فایل برنامه را از شما بگیرد.',
		noAccounts: 'هنوز حسابی نیست', noAccountsHint: 'برای هر کسی که باید از اینترنت این روتر استفاده کند یک حساب بسازید.',
		onlineNow: 'الان وصل است ({0})', lastSeen: 'آخرین استفاده {0}', neverSeen: 'هنوز استفاده نشده',
		pausedLimit: 'متوقف: حجمش تمام شد', pausedByYou: 'متوقف', accountOn: 'حساب روشن',
		removeAccount: 'برای حذف حساب {0} دوباره بزنید', accountName: 'برای چه کسی', accountNamePh: 'نام، مثلاً سارا',
		limitGB: 'سقف حجم (گیگابایت)', noLimit: 'بدون سقف', resetUsage: 'صفر کردن مصرف', newLink: 'لینک جدید (لینک قبلی دیگر کار نمی‌کند)',
		addAccountHint: 'برای این نفر یک لینک جدا ساخته می‌شود؛ بلافاصله بعدش می‌توانید بفرستید.', newLinkDone: 'لینک جدید ساخته شد: دوباره بفرستید',
		copyMessage: 'کپی پیام',
		howToUse: 'این لینک فقط برای شماست؛ آن را به کسی ندهید.\nاندروید: v2rayNG یا Hiddify را نصب کنید، لینک زیر را کپی کنید، در برنامه «Import from clipboard» را بزنید و وصل شوید.\nآیفون: Streisand یا V2Box را نصب کنید و همین کار را بکنید.\nاگر وصل نشد به من خبر بدهید.',
		sharingPort: 'پورت', sharingPortHint: 'اگر مودم داخلی خودش روتر است، این پورت TCP را روی آن به این روتر فوروارد کنید',
		sharingSni: 'شبیه بازدید از', sharingSniHint: 'یک سایت پربازدید با TLS 1.3. هر کس پورت را بررسی کند همان سایت را می‌بیند.',
		sharingOn: 'در حال اجرا', sharingDown: 'هنوز اجرا نشده؛ گزارش را ببینید', sharingAt: 'بقیه به {0} وصل می‌شوند (روی {1})',
		sharingNoAddr: 'روتر هنوز آدرس اینترنت ندارد', showLink: 'لینک و QR', newKey: 'کلید جدید',
		dualwanOn: 'دو پورت اینترنت روشن است: پورت دوم (wan2) اینترنت داخلی است و آدرس‌های ایرانی از آن می‌روند، بقیه از پورت اول. برای برگرداندن، در SSH:',
		dualwanOff: 'برای اینکه پورت دوم روتر اینترنت داخلی بگیرد و پورت اول استارلینک باشد، این را در SSH بزنید. پورت LAN سیمی دیگر LAN نیست؛ وای‌فای می‌ماند.',
		sBackup: 'پشتیبان', backup: 'پشتیبان از همه چیز', backupHint: 'سرورها، اشتراک‌ها، قوانین، دستگاه‌ها و تنظیمات در یک فایل. آن را به کسی ندهید: کلیدهای سرورهایتان در آن است.',
		download: 'دانلود', restore: 'بازگردانی از فایل', restoreHint: 'همه چیز اینجا با محتوای فایل جایگزین می‌شود',
		chooseFile: 'انتخاب فایل', notBackup: 'این فایل پشتیبان فاکس‌گذر نیست', restored: 'بازگردانی شد: {0} سرور، {1} اشتراک، {2} دستگاه',
		installed: 'نصب‌شده: {0}', notInstalled: 'نصب نیست', updateCore: 'به‌روزرسانی {0}', updateRules: 'به‌روزرسانی rule-setها',
		updating: 'در حال به‌روزرسانی…', theme: 'تم', lang: 'English',
		offline: 'سرویس فاکس‌گذر جواب نمی‌دهد. شاید در حال اجراست؛ این صفحه خودش دوباره تلاش می‌کند.',
		justNow: 'همین الان', minAgo: '{0} دقیقه پیش', hAgo: '{0} ساعت پیش', dAgo: '{0} روز پیش'
	}
};

var lang = 'fa';
function t(k) {
	var s = (T[lang] && T[lang][k]) || T.en[k] || k;
	for (var i = 1; i < arguments.length; i++)
		s = s.replace('{' + (i - 1) + '}', num(arguments[i]));
	return s;
}
function num(v) {
	if (typeof v !== 'number') return v;
	return lang === 'fa' ? v.toLocaleString('fa-IR') : v.toLocaleString('en-US');
}

/* ------------------------------------------------------------ helpers */

// replaceChildren() turns null into the text "null"; build lists through this.
function present(list) { return list.filter(function(k) { return k != null && k !== false; }); }

function h(tag, attrs, kids) {
	var el = document.createElement(tag);
	for (var k in (attrs || {})) {
		var v = attrs[k];
		if (v == null || v === false) continue;
		if (k.slice(0, 2) === 'on') el.addEventListener(k.slice(2), v);
		else if (k === 'html') el.innerHTML = v;
		else el.setAttribute(k, v === true ? '' : v);
	}
	[].concat(kids == null ? [] : kids).forEach(function(c) {
		if (c == null || c === false) return;
		el.appendChild(c instanceof Node ? c : document.createTextNode(String(c)));
	});
	return el;
}

function api(method, path, body) {
	var opts = { method: method, headers: { 'X-Luci-Session': SID } };
	if (body !== undefined) {
		opts.headers['Content-Type'] = 'application/json';
		opts.body = JSON.stringify(body);
	}
	return fetch(BASE + path + (path.indexOf('?') < 0 ? '?' : '&') + 'sid=' + encodeURIComponent(SID), opts).then(function(r) {
		return r.json().catch(function() { return {}; }).then(function(j) {
			if (!r.ok) throw new Error(j.error || ('HTTP ' + r.status));
			return j;
		});
	});
}

function bytes(n) {
	var u = ['B', 'KB', 'MB', 'GB', 'TB'], i = 0;
	n = n || 0;
	while (n >= 1024 && i < u.length - 1) { n /= 1024; i++; }
	return num(+(n < 10 && i ? n.toFixed(1) : Math.round(n))) + ' ' + u[i];
}
function rate(n) { return bytes(n) + '/s'; }

function ago(unix) {
	if (!unix) return '';
	var s = Date.now() / 1000 - unix;
	if (s < 60) return t('justNow');
	if (s < 3600) return t('minAgo', Math.round(s / 60));
	if (s < 86400) return t('hAgo', Math.round(s / 3600));
	return t('dAgo', Math.round(s / 86400));
}

function duration(sec) {
	sec = Math.max(0, Math.floor(sec));
	var hh = Math.floor(sec / 3600), mm = Math.floor(sec % 3600 / 60), ss = sec % 60;
	var p = function(x) { return (x < 10 ? '0' : '') + x; };
	var s = (hh ? hh + ':' + p(mm) : mm) + ':' + p(ss);
	return lang === 'fa' ? s.replace(/\d/g, function(d) { return '۰۱۲۳۴۵۶۷۸۹'[d]; }) : s;
}

function quality(d) {
	if (d == null || d === 0) return '';
	if (d < 0) return 'fail';
	if (d < 400) return 'good';
	if (d < 1000) return 'mid';
	return 'bad';
}

var toastTimer;
function toast(msg) {
	var old = document.querySelector('.fgz-toast');
	if (old) old.remove();
	var el = h('div', { 'class': 'fgz-toast', role: 'status' }, msg);
	document.querySelector('.fgz').appendChild(el);
	clearTimeout(toastTimer);
	toastTimer = setTimeout(function() { el.remove(); }, 3500);
}

function busy(btn, p) {
	btn.setAttribute('aria-busy', 'true');
	btn.disabled = true;
	return p.finally(function() { btn.removeAttribute('aria-busy'); btn.disabled = false; });
}

var ICON = {
	gauge: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M4 18a9 9 0 1 1 16 0"/><path d="m12 14 4-5"/></svg>',
	clock: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/></svg>',
	power: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round"><path d="M12 3v8"/><path d="M6.3 6.8a8 8 0 1 0 11.4 0"/></svg>',
	bolt: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linejoin="round"><path d="M13 3 5 14h6l-1 7 8-11h-6z"/></svg>',
	pencil: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M4 20h4L19 9l-4-4L4 16z"/><path d="m13 7 4 4"/></svg>',
	qr: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><rect x="3" y="3" width="7" height="7" rx="1"/><rect x="14" y="3" width="7" height="7" rx="1"/><rect x="3" y="14" width="7" height="7" rx="1"/><path d="M14 14h3v3h-3zM20 14v1M14 20h1M18 20h3v-3"/></svg>',
	trash: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M4 7h16M10 11v6M14 11v6M6 7l1 13h10l1-13M9 7V4h6v3"/></svg>',
	refresh: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M20 11a8 8 0 1 0-2.3 5.7M20 4v7h-7"/></svg>',
	fox: '<svg viewBox="0 0 48 48"><path d="M6 6l12 9h12l12-9-3 18-15 18L9 24z" fill="#F99532"/><path d="M18 15h12l-6 8z" fill="#fff" opacity=".9"/><path d="M15 26l9 16 9-16-9 5z" fill="#fff"/><circle cx="17.5" cy="22" r="2" fill="#0E1730"/><circle cx="30.5" cy="22" r="2" fill="#0E1730"/><path d="M22 37h4l-2 3z" fill="#0E1730"/></svg>'
};
function icon(name) { var s = h('span', { 'aria-hidden': 'true', html: ICON[name] }); return s.firstChild; }

/* ------------------------------------------------------------ the app */

var S = {
	tab: 'nodes', status: null, nodes: [], devices: [], hist: [], games: null, showTable: false, subs: [], rules: [], settings: null, cores: {},
	speed: { up: 0, down: 0 }, filter: '', offline: false, logSeq: 0, logLines: []
};
var root, heroEl, statsEl, panelEl, tabsEl;

function savedTheme() {
	try { return localStorage.getItem('fgz-theme'); } catch (e) { return null; }
}
function applyTheme() {
	var th = savedTheme();
	if (!th) th = matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
	root.setAttribute('data-theme', th);
}

function render() {
	root.setAttribute('dir', lang === 'fa' ? 'rtl' : 'ltr');
	root.setAttribute('lang', lang);
	root.replaceChildren(
		h('div', { 'class': 'fgz-head' }, [
			h('div', { 'class': 'fgz-brand' }, [icon('fox'), lang === 'fa' ? 'فاکس‌گذر' : 'FoxGozar']),
			h('button', { 'class': 'fgz-chip', onclick: function() {
				var next = root.getAttribute('data-theme') === 'dark' ? 'light' : 'dark';
				try { localStorage.setItem('fgz-theme', next); } catch (e) {}
				root.setAttribute('data-theme', next);
			} }, t('theme')),
			h('button', { 'class': 'fgz-chip', onclick: function() {
				lang = lang === 'fa' ? 'en' : 'fa';
				api('PUT', '/api/settings', { language: lang }).catch(function() {});
				render();
			} }, t('lang'))
		]),
		heroEl = h('section', { 'class': 'fgz-hero', 'aria-live': 'polite' }),
		statsEl = h('section', { 'class': 'fgz-cards' }),
		tabsEl = h('nav', { 'class': 'fgz-tabs', role: 'tablist' }),
		panelEl = h('section', { role: 'tabpanel' })
	);
	renderHero();
	renderTabs();
	renderPanel();
}

/* ---------- hero: the passage */

function renderHero() {
	var st = S.status || { phase: 'stopped' };
	var phase = st.phase;
	root.setAttribute('data-phase', phase);
	var on = phase === 'connected' || phase === 'starting' || phase === 'installing';
	var title = { connected: t('connected'), starting: t('starting'), installing: t('installing'), failed: t('failed') }[phase] || t('disconnected');
	var sub = phase === 'connected' && st.backup ? t('viaWarp')
		: phase === 'connected' && st.current_name ? t('via', st.current_name)
		: (phase === 'installing' && st.message) ? st.message
		: phase === 'failed' ? '' : t('idle');

	var power = h('button', {
		'class': 'fgz-power', 'data-on': on ? '1' : '0',
		'aria-label': on ? t('disconnect') : t('connect'), title: on ? t('disconnect') : t('connect'),
		onclick: function() {
			var p = api('POST', on ? '/api/stop' : '/api/start')
				.then(function(s) { S.status = s; renderHero(); })
				.catch(function(e) { toast(e.message); refreshStatus(); });
			busy(power, p);
		}
	}, [icon('power')]);

	// Connected: the status goes into the pill and the server becomes the
	// title, with its protocol and delay under it.
	var pill = h('span', { 'class': 'fgz-pill' }, [h('i'), title, h('span', { 'class': 'ver' }, '· ' + (lang === 'fa' ? 'فاکس‌گذر' : 'FoxGozar') + ' ' + VERSION)]);
	if (phase === 'connected') {
		var node = (S.nodes || []).filter(function(n) { return n.id === st.current; })[0];
		title = st.backup ? 'WARP' : (st.current_name || title);
		sub = st.backup ? t('viaWarp') : present([node && node.type, st.delay ? num(st.delay) + ' ms' : null]).join(' · ');
	}

	heroEl.replaceChildren.apply(heroEl, present([
		h('div', { 'class': 'fgz-dial' }, [h('span', { html: RINGS, style: 'display:contents' }), power]),
		h('div', { 'class': 'fgz-info' }, present([
		h('div', { 'class': 'fgz-state' }, [pill, h('h2', { dir: 'auto', 'class': phase === 'connected' ? 'srv' : null }, title), sub ? h('p', { dir: phase === 'connected' ? 'ltr' : null, 'class': phase === 'connected' ? 'num' : null }, sub) : null]),
		h('div', { 'class': 'fgz-passage', 'aria-hidden': 'true' }, [
			h('div', { 'class': 'fgz-end' }, [h('b', {}, t('yourNet'))]),
			h('div', { 'class': 'fgz-mid' }, [
				phase === 'connected' ? h('div', { 'class': 'fgz-speed num', id: 'fgz-speed', dir: 'ltr' }, speedKids()) : null,
				h('div', { 'class': 'fgz-track' }, [h('i'), h('i')])
			]),
			h('div', { 'class': 'fgz-end' }, [h('b', {}, phase === 'connected' ? (st.backup ? 'WARP' : countryOf(st.current_name) || t('exit')) : t('exit'))])
		])
		])),
		S.settings ? quickControls() : null,
		phase === 'failed' && st.message ? h('div', { 'class': 'fgz-alert', role: 'alert' }, st.message) : null,
		subWarning(),
		S.offline ? h('div', { 'class': 'fgz-alert', role: 'alert' }, t('offline')) : null
	]));
	renderStats();
}

// The settings people change most, one press away: segmented buttons.
function quickControls() {
	var s = S.settings, st = S.status || {};
	var seg = function(label, opts, cur, pick) {
		return h('div', { 'class': 'fgz-q' }, [
			h('span', {}, label),
			h('div', { 'class': 'fgz-seg', role: 'radiogroup', 'aria-label': label }, opts.map(function(o) {
				return h('button', { type: 'button', role: 'radio', 'aria-checked': o[0] === cur ? 'true' : 'false',
					onclick: function() { if (o[0] !== cur) pick(o[0]); } }, o[1]);
			}))
		]);
	};
	var save = function(patch) { saveSetting(patch).then(renderHero); };
	return h('div', { 'class': 'fgz-quick' }, [
		h('small', { 'class': 'fgz-quick-title' }, t('quick')),
		seg(t('qServer'), [['auto', t('qAuto')], ['manual', t('qManual')]], s.mode === 'manual' ? 'manual' : 'auto', function(v) {
			if (v === 'auto') choose('auto');
			else if (st.current || (S.nodes && S.nodes[0])) choose(st.current || S.nodes[0].id);
		}),
		seg(t('qIran'), [[true, t('qDirect')], [false, t('qTunnel')]], !!s.iran_direct, function(v) { save({ iran_direct: v }); }),
		seg(t('killSwitch'), [[true, t('on')], [false, t('off')]], !!s.kill_switch, function(v) { save({ kill_switch: v }); })
	]);
}

/* ---------- status cards under the hero */

function card(label, big, rows, cls) {
	return h('div', { 'class': 'fgz-card' + (cls ? ' ' + cls : '') }, [
		h('small', { 'class': 'lbl' }, label),
		h('div', { 'class': 'big num' }, h('span', { dir: 'ltr' }, big)),
		h('dl', {}, rows.filter(Boolean).map(function(r) { return h('div', {}, [h('dt', {}, r[0]), h('dd', { 'class': 'num', dir: 'ltr' }, r[1])]); }))
	]);
}

function renderStats() {
	if (!statsEl) return;
	var st = S.status || {}, on = st.phase === 'connected';
	var dash = '—';
	var delays = (S.nodes || []).map(function(n) { return n.delay; }).filter(function(d) { return d > 0; });
	var best = delays.length ? Math.min.apply(null, delays) : 0;
	var avg = delays.length ? Math.round(delays.reduce(function(a, b) { return a + b; }, 0) / delays.length) : 0;

	var sub = (S.subs || []).filter(function(x) { return x.enabled && x.total; })[0] || (S.subs || [])[0];
	var last;
	if (sub) {
		var used = (sub.upload || 0) + (sub.download || 0);
		last = h('div', { 'class': 'fgz-card' }, [
			h('small', { 'class': 'lbl' }, t('cSub')),
			h('div', { 'class': 'big name', dir: 'auto' }, sub.name),
			sub.total ? h('div', { 'class': 'fgz-meter' }, [h('i', { style: 'width:' + Math.min(100, used / sub.total * 100).toFixed(1) + '%' })]) : null,
			h('dl', {}, present([
				sub.total ? h('div', {}, [h('dt', {}, t('cQuota')), h('dd', { 'class': 'num', dir: 'ltr' }, bytes(used) + ' / ' + bytes(sub.total))]) : null,
				h('div', {}, [h('dt', {}, t('cServers')), h('dd', { 'class': 'num' }, num(sub.node_count || 0))])
			]))
		].filter(Boolean));
	} else {
		var ok = (S.nodes || []).filter(function(n) { return n.delay > 0; }).length;
		last = card(t('cServers'), num((S.nodes || []).length), [[t('cWorking'), num(ok)]]);
	}

	statsEl.replaceChildren(
		card(t('cConn'), on ? h('span', { id: 'fgz-uptime' }, duration(Date.now() / 1000 - st.since)) : dash, [
			[t('conns'), on ? num(st.connections || 0) : dash],
			[t('cMemory'), on && st.memory ? bytes(st.memory) : dash]
		]),
		card(t('delay'), on && st.delay ? num(st.delay) + ' ms' : dash, [
			[t('cBest'), best ? num(best) + ' ms' : dash],
			[t('cAvg'), avg ? num(avg) + ' ms' : dash]
		]),
		h('div', { 'class': 'fgz-card' }, [
			h('small', { 'class': 'lbl' }, t('cTraffic')),
			h('div', { 'class': 'fgz-flow' }, [
				h('div', {}, [h('small', {}, t('cNow')), h('div', { id: 'fgz-live', 'class': 'num', dir: 'ltr' }, liveKids())]),
				h('div', {}, [h('small', {}, t('cTotal')), h('div', { 'class': 'num', dir: 'ltr' }, [
					h('b', { 'class': 'dn' }, 'Σ↓ ' + (on ? bytes(st.download || 0) : dash)),
					h('b', { 'class': 'up' }, 'Σ↑ ' + (on ? bytes(st.upload || 0) : dash))
				])])
			]),
			S.usage && (S.usage.up || S.usage.down) ? h('dl', {}, [h('div', { title: t('monthHint') }, [
				h('dt', {}, t('cMonth')), h('dd', { 'class': 'num', dir: 'ltr' }, bytes(S.usage.up + S.usage.down))
			])]) : null
		].filter(Boolean)),
		last
	);
}

function liveKids() {
	return [h('b', { 'class': 'dn' }, '↓ ' + rate(S.speed.down)), h('b', { 'class': 'up' }, '↑ ' + rate(S.speed.up))];
}

// A subscription about to run out, by date or by traffic: better known a
// few days before than when the servers stop answering.
function subWarning() {
	var notes = [];
	(S.subs || []).forEach(function(s) {
		if (!s.enabled) return;
		// Isolated, so a Latin name keeps its place inside Persian text.
		var name = '⁨' + s.name + '⁩';
		var used = (s.upload || 0) + (s.download || 0);
		if (s.expire) {
			var days = Math.floor((s.expire * 1000 - Date.now()) / 864e5);
			if (days < 0) { notes.push(t('subExpired', name)); return; }
			if (days <= 3) { notes.push(t('subExpiresSoon', name, days)); return; }
		}
		if (s.total && used / s.total >= 0.9)
			notes.push(t('subAlmostUsed', name, Math.round(used / s.total * 100)));
	});
	return notes.length ? h('div', { 'class': 'fgz-alert warn' }, notes.map(function(n) { return h('div', {}, n); })) : null;
}

function speedKids() {
	return [
		h('span', {}, ['↑ ', rate(S.speed.up)]),
		h('span', {}, ['↓ ', rate(S.speed.down)])
	];
}

// Server names often start with the country ("Germany-WS-…").
function countryOf(name) {
	var m = /^([A-Za-z؀-ۿ ]{3,20}?)[-_ |]/.exec(name || '');
	return m ? m[1] : '';
}

// Signal rings around the connect button: waves go out while connected, an
// arc turns while connecting, red on failure. CSS drives it by phase.
var RINGS = '<svg class="fgz-rings" viewBox="0 0 200 200" aria-hidden="true">' +
	'<circle class="r3" cx="100" cy="100" r="96"/><circle class="r2" cx="100" cy="100" r="80"/>' +
	'<circle class="r1" cx="100" cy="100" r="64"/>' +
	'<circle class="wave" cx="100" cy="100" r="64"/><circle class="wave w2" cx="100" cy="100" r="64"/>' +
	'<circle class="arc" cx="100" cy="100" r="80"/></svg>';

/* ---------- tabs */

var TABS = ['nodes', 'network', 'devices', 'games', 'subs', 'rules', 'share', 'settings', 'logs'];
function renderTabs() {
	tabsEl.replaceChildren.apply(tabsEl, TABS.map(function(k) {
		return h('button', {
			role: 'tab', 'aria-selected': S.tab === k ? 'true' : 'false',
			onclick: function() { S.tab = k; renderTabs(); loadTab(); }
		}, t(k));
	}));
}

function loadTab() {
	var p;
	switch (S.tab) {
	case 'nodes': p = Promise.all([api('GET', '/api/nodes'), api('GET', '/api/subs'), api('GET', '/api/services'), api('GET', '/api/speedtest'), api('GET', '/api/usage'), api('GET', '/api/uptime')]).then(function(r) { S.nodes = r[0]; S.subs = r[1]; S.services = r[2]; S.speed2 = r[3]; S.usage = r[4]; S.uptime = r[5]; }); break;
	case 'subs': p = api('GET', '/api/subs').then(function(r) { S.subs = r; }); break;
	case 'rules': p = Promise.all([api('GET', '/api/rules'), api('GET', '/api/nodes')]).then(function(r) { S.rules = r[0]; S.nodes = r[1]; }); break;
	case 'settings': p = Promise.all([api('GET', '/api/core'), api('GET', '/api/warp')]).then(function(r) { S.cores = r[0]; S.warp = r[1]; }); break;
	case 'share': p = api('GET', '/api/sharing').then(function(r) { S.sharing = r; }); break;
	case 'network': p = api('GET', '/api/devices').then(function(r) { S.devices = r; }); break;
	case 'games': p = Promise.all([api('GET', '/api/games'), api('GET', '/api/nodes'), api('GET', '/api/antilag')]).then(function(r) { S.games = r[0]; S.nodes = r[1]; S.lag = r[2]; }); break;
	case 'devices': p = Promise.all([api('GET', '/api/devices'), api('GET', '/api/nodes')]).then(function(r) { S.devices = r[0]; S.nodes = r[1]; }); break;
	case 'logs': S.logSeq = 0; S.logLines = []; p = pollLogs(); break;
	}
	renderPanel();
	return (p || Promise.resolve()).then(renderPanel).catch(function(e) { toast(e.message); });
}

function renderPanel() {
	var fn = { nodes: panelNodes, network: panelNetwork, games: panelGames, devices: panelDevices, subs: panelSubs, rules: panelRules, share: panelShare, settings: panelSettings, logs: panelLogs }[S.tab];
	panelEl.replaceChildren(fn());
	// The charts need the panel in the page to know their width.
	if (S.tab === 'network') { drawTraffic(); drawMap(); }
}

/* ---------- servers */

function panelNodes() {
	var st = S.status || {};
	var manual = S.settings && S.settings.mode === 'manual';
	var selected = manual ? S.settings.selected : 'auto';
	var q = S.filter.trim().toLowerCase();

	var bar = h('div', { 'class': 'fgz-bar' }, [
		h('input', { type: 'text', 'class': 'grow', placeholder: t('search'), value: S.filter,
			oninput: function(e) { S.filter = e.target.value; var c = e.target.selectionStart; renderPanel(); var i = panelEl.querySelector('input'); i.focus(); i.setSelectionRange(c, c); } }),
		h('button', { 'class': 'fgz-btn primary', onclick: dialogAdd }, t('add')),
		testAllBtn(),
		servicesBtn(),
		speedAllBtn(),
		h('button', { 'class': 'fgz-btn danger', onclick: function(e) {
			busy(e.currentTarget, api('POST', '/api/nodes/delete', { failed: true }).then(function(r) {
				toast(t('removed', r.deleted || 0)); return loadTab();
			}).catch(function(err) { toast(err.message); }));
		} }, t('removeFailed'))
	]);

	if (!S.nodes.length)
		return h('div', { 'class': 'fgz-panel' }, [bar, h('div', { 'class': 'fgz-empty' }, [h('b', {}, t('noNodes')), t('noNodesHint')])]);

	var groups = {}, order = [];
	S.nodes.forEach(function(n) {
		if (q && (n.name + ' ' + n.type + ' ' + n.server).toLowerCase().indexOf(q) < 0) return;
		var g = n.group || t('addedByYou');
		if (!groups[g]) { groups[g] = []; order.push(g); }
		groups[g].push(n);
	});

	var kids = [bar];
	if (!q) kids.push(row('auto', t('auto'), t('autoHint'), null, selected === 'auto', false));
	order.forEach(function(g) {
		if (order.length > 1 || g !== t('addedByYou')) kids.push(h('div', { 'class': 'fgz-group' }, g));
		// The fastest of this subscription only (the router builds the group
		// for subscriptions with two servers or more).
		var subId = groups[g][0].sub, all = S.nodes.filter(function(n) { return n.sub && n.sub === subId; });
		if (!q && subId && all.length >= 2)
			kids.push(row('auto-' + subId, t('autoSub'), t('autoSubHint'), null, selected === 'auto-' + subId, false));
		groups[g].forEach(function(n) {
			var live = st.phase === 'connected' && st.current === n.id;
			kids.push(row(n.id, n.name, n.type + (n.server ? '  ' + n.server : ''), n.delay, selected === n.id, live));
		});
	});
	return h('div', { 'class': 'fgz-panel' }, kids);
}

function testAllBtn() {
	var b = h('button', { 'class': 'fgz-btn', onclick: function() {
		if (!S.status || S.status.phase !== 'connected') { toast(t('connectFirst')); return; }
		b.textContent = t('testing');
		busy(b, api('POST', '/api/test').then(function(res) {
			var ok = 0, all = 0;
			S.nodes.forEach(function(n) { if (n.id in res) { n.delay = res[n.id]; all++; if (res[n.id] > 0) ok++; } });
			toast(t('tested', ok, all));
			renderPanel();
		}).catch(function(e) { toast(e.message); b.textContent = t('testAll'); }));
	} }, t('testAll'));
	return b;
}

function row(id, name, sub, delay, checked, live) {
	var q = quality(delay);
	var delayText = delay > 0 ? num(delay) + ' ms' : delay < 0 ? t('failedShort') : '';
	var el = h('div', {
		'class': 'fgz-row', role: 'radio', tabindex: '0', 'aria-checked': checked ? 'true' : 'false',
		onclick: function() { choose(id); },
		onkeydown: function(e) { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); choose(id); } }
	}, [
		h('span', { 'class': 'dot' }),
		h('div', { 'class': 'name' }, [h('b', { 'class': live ? 'live' : null, dir: 'auto' }, name), sub ? h('small', { dir: id.indexOf('auto') === 0 ? 'auto' : 'ltr' }, sub) : null, serviceChips(id), speedLine(id), uptimeStrip(id)]),
		id.indexOf('auto') === 0 || !delayText ? h('span') : h('span', { 'class': 'fgz-delay num', 'data-q': q, dir: delay > 0 ? 'ltr' : null }, delayText),
		id.indexOf('auto') === 0 ? h('span') : h('span', { style: 'display:flex' }, [
			h('button', { 'class': 'fgz-icon', title: t('test'), 'aria-label': t('test'), onclick: function(e) {
				e.stopPropagation();
				if (!S.status || S.status.phase !== 'connected') { toast(t('connectFirst')); return; }
				busy(e.currentTarget, api('POST', '/api/test/' + id).then(function(r) {
					S.nodes.forEach(function(n) { if (n.id === id) n.delay = r.delay; });
					renderPanel();
				}));
			} }, [icon('bolt')]),
			h('button', { 'class': 'fgz-icon', title: t('speedTest'), 'aria-label': t('speedTest'), onclick: function(e) {
				e.stopPropagation();
				runSpeed([id], e.currentTarget);
			} }, [icon('gauge')]),
			h('button', { 'class': 'fgz-icon', title: t('share'), 'aria-label': t('share'), onclick: function(e) {
				e.stopPropagation();
				showShare(id, name);
			} }, [icon('qr')]),
			h('button', { 'class': 'fgz-icon', title: t('remove'), 'aria-label': t('remove'), onclick: function(e) {
				e.stopPropagation();
				api('DELETE', '/api/nodes/' + id).then(loadTab).catch(function(err) { toast(err.message); });
			} }, [icon('trash')])
		])
	]);
	return el;
}

// The last speed test: download speed, and a warning when new connections
// to the server come up unsteadily (some take seconds).
function speedLine(id) {
	if (id.indexOf('auto') === 0) return null;
	var r = S.speed2 && S.speed2[id];
	var used = S.usage && S.usage.nodes && S.usage.nodes[id];
	if (!r && !used) return null;
	return h('span', { 'class': 'fgz-svcs', dir: 'ltr' }, present([
		used ? h('span', { 'class': 'fgz-svc', 'data-ok': '1', title: t('monthHint') }, t('thisMonth', bytes(used[0] + used[1]))) : null,
		r ? h('span', { 'class': 'fgz-svc', 'data-ok': r.mbps > 0 ? '1' : '0', title: t('speedHint') },
			r.mbps > 0 ? num(Math.round(r.mbps * 10) / 10) + ' Mbps' : t('speedFailed')) : null,
		r && r.unstable ? h('span', { 'class': 'fgz-svc', 'data-ok': '0', title: t('unstableHint', r.slow, r.setup.length, num(r.best)) }, t('unstable')) : null
	]));
}

// The server's last 24 hours as 48 half-hour bars (up, partly down, down,
// not measured) and its share of answered checks over the week.
function uptimeStrip(id) {
	var u = S.uptime && S.uptime[id];
	if (!u || id.indexOf('auto') === 0 || !u.marks) return null;
	var per = Math.round(1800 / u.step), want = 48 * per;
	var m = u.marks.slice(-want);
	while (m.length < want) m = '-' + m;
	var bars = [];
	for (var i = 0; i < 48; i++) {
		var chunk = m.substr(i * per, per), up = (chunk.match(/u/g) || []).length, down = (chunk.match(/d/g) || []).length;
		bars.push(h('i', { 'data-s': up + down === 0 ? 'n' : down === 0 ? 'u' : up === 0 ? 'd' : 'p' }));
	}
	return h('span', { 'class': 'fgz-uptime', dir: 'ltr', title: t('uptimeHint') }, [
		h('span', { 'class': 'bars' }, bars),
		h('small', { 'class': 'num' }, u.pct >= 0 ? t('uptimePct', num(u.pct)) : t('uptimeNone'))
	]);
}

// Speed tests run one server at a time: in parallel they would share the
// line and each look slower than it is.
function runSpeed(ids, btn) {
	if (!S.status || S.status.phase !== 'connected') { toast(t('connectFirst')); return; }
	S.speed2 = S.speed2 || {};
	var label = btn.textContent, i = 0;
	var next = function() {
		if (i >= ids.length) return Promise.resolve();
		var id = ids[i++];
		if (ids.length > 1) btn.textContent = t('speedTesting', i, ids.length);
		return api('POST', '/api/speedtest/' + id).then(function(r) { S.speed2[id] = r; renderPanel(); })
			.catch(function(err) { toast(err.message); }).then(next);
	};
	busy(btn, next().then(function() { if (ids.length > 1) { btn.textContent = label; toast(t('speedDone')); } }));
}

function speedAllBtn() {
	var b = h('button', { 'class': 'fgz-btn', title: t('speedAllHint'), onclick: function() {
		runSpeed(S.nodes.map(function(n) { return n.id; }), b);
	} }, t('speedAll'));
	return b;
}

// Which of the usual sites a server opens, from the last service test.
function serviceChips(id) {
	var res = S.services && S.services.results && S.services.results[id];
	if (!res || id === 'auto') return null;
	return h('span', { 'class': 'fgz-svcs', dir: 'ltr' }, S.services.services.map(function(s) {
		var d = res[s.id];
		if (d == null) return null;
		return h('span', { 'class': 'fgz-svc', 'data-ok': d > 0 ? '1' : '0',
			title: s.name + ': ' + (d > 0 ? num(d) + ' ms' : t('failedShort')) }, s.name);
	}));
}

function servicesBtn() {
	var b = h('button', { 'class': 'fgz-btn', title: t('testServicesHint'), onclick: function() {
		if (!S.status || S.status.phase !== 'connected') { toast(t('connectFirst')); return; }
		b.textContent = t('testing');
		busy(b, api('POST', '/api/services/test').then(function(r) {
			S.services = r;
			renderPanel();
		}).catch(function(e) { toast(e.message); b.textContent = t('testServices'); }));
	} }, t('testServices'));
	return b;
}

function showShare(id, name) {
	api('GET', '/api/nodes/' + id + '/share').then(function(r) { linkDialog(name, r); }).catch(function(e) { toast(e.message); });
}

// A link with its QR code (r.link, r.svg), ready to copy or scan. With
// guide, a second button copies a whole message: the guide, then the link.
function linkDialog(name, r, guide) {
	var dlg = h('dialog', { 'class': 'fgz-dialog' });
	var ta = h('textarea', { readonly: true, rows: 3, spellcheck: 'false' }, r.link);
	var copyBtn = function(label, text, primary) {
		var b = h('button', { 'class': 'fgz-btn' + (primary ? ' primary' : ''), type: 'button', onclick: function() {
			var done = function() { b.textContent = t('copied'); };
			if (navigator.clipboard && window.isSecureContext)
				navigator.clipboard.writeText(text).then(done);
			else {
				var tmp = h('textarea', { style: 'position:fixed;opacity:0' }, text);
				dlg.appendChild(tmp); tmp.select(); document.execCommand('copy'); tmp.remove(); done();
			}
		} }, label);
		return b;
	};
	dlg.appendChild(h('form', { method: 'dialog' }, present([
		h('h3', { dir: 'auto' }, name),
		r.svg ? h('div', { 'class': 'fgz-qr', html: r.svg, role: 'img', 'aria-label': 'QR' }) : null,
		h('p', {}, r.svg ? t('scanHint') : t('qrTooLong')),
		ta,
		guide ? h('p', { 'class': 'fgz-guide' }, guide) : null,
		h('div', { 'class': 'fgz-actions' }, present([
			h('button', { 'class': 'fgz-btn', value: 'close' }, t('close')),
			guide ? copyBtn(t('copyMessage'), guide + '\n\n' + r.link, false) : null,
			copyBtn(t('copy'), r.link, true)
		]))
	])));
	dlg.addEventListener('close', function() { dlg.remove(); });
	root.appendChild(dlg);
	dlg.showModal();
}

function choose(id) {
	api('POST', '/api/select', { id: id }).then(function() {
		S.settings.mode = id === 'auto' ? 'auto' : 'manual';
		S.settings.selected = id === 'auto' ? '' : id;
		renderPanel();
		setTimeout(refreshStatus, 400);
	}).catch(function(e) { toast(e.message); });
}

function dialog(title, hint, fields, okText, onOk) {
	var dlg = h('dialog', { 'class': 'fgz-dialog' });
	var form = h('form', { method: 'dialog' }, [h('h3', {}, title), hint ? h('p', {}, hint) : null].concat(fields).concat([
		h('div', { 'class': 'fgz-actions' }, [
			h('button', { 'class': 'fgz-btn', value: 'cancel', formnovalidate: true }, t('cancel')),
			h('button', { 'class': 'fgz-btn primary', value: 'ok' }, okText)
		])
	]));
	form.addEventListener('submit', function(e) {
		if (e.submitter && e.submitter.value === 'cancel') return;
		e.preventDefault();
		var btn = e.submitter;
		busy(btn, Promise.resolve(onOk()).then(function() { dlg.close(); }).catch(function(err) { toast(err.message); }));
	});
	dlg.appendChild(form);
	dlg.addEventListener('close', function() { dlg.remove(); });
	root.appendChild(dlg);
	dlg.showModal();
	var first = dlg.querySelector('textarea, input');
	if (first) first.focus();
}

function dialogAdd() {
	var ta = h('textarea', { required: true, spellcheck: 'false', placeholder: 'vless://…' });
	dialog(t('addTitle'), t('addHint'), [ta], t('add'), function() {
		return api('POST', '/api/nodes', { text: ta.value }).then(function(r) {
			var msg = t('added', r.added || 0);
			if (r.skipped && r.skipped.length) msg += (lang === 'fa' ? '، ' : ', ') + t('skipped', r.skipped.length);
			toast(msg);
			return loadTab();
		});
	});
}

/* ---------- devices */

function panelDevices() {
	var def = (S.settings && S.settings.default_policy) || 'proxy';
	var bar = h('div', { 'class': 'fgz-bar' }, [
		h('label', { 'for': 'fgz-defpol', 'class': 'grow' }, t('newDevices')),
		h('select', { id: 'fgz-defpol', style: 'width:auto', onchange: function(e) {
			saveSetting({ default_policy: e.target.value }).then(loadTab);
		} }, [['proxy', t('viaTunnel')], ['direct', t('direct')]].map(function(o) {
			return h('option', { value: o[0], selected: def === o[0] }, o[1]);
		}))
	]);
	if (!S.devices.length)
		return h('div', { 'class': 'fgz-panel' }, [bar, h('div', { 'class': 'fgz-empty' }, [h('b', {}, t('noDevices')), t('noDevicesHint')])]);
	return h('div', { 'class': 'fgz-panel' }, [bar].concat(S.devices.map(deviceRow)));
}

function deviceRow(d) {
	var value = d.policy === 'node' ? 'node:' + d.node : d.policy;
	var options = [['proxy', t('viaTunnel')], ['direct', t('direct')], ['block', t('block')]];
	var servers = S.nodes.map(function(n) { return ['node:' + n.id, n.name]; });

	var usage;
	if (d.policy === 'direct' || (d.policy === 'block'))
		usage = h('small', {}, d.policy === 'direct' ? t('directNotMeasured') : '');
	else
		usage = h('small', { 'class': 'num', dir: 'ltr', title: t('usageHint') }, [
			'↓ ' + bytes(d.down) + '  ↑ ' + bytes(d.up),
			(d.rate_down || d.rate_up) ? '   (' + rate(d.rate_down) + ')' : ''
		]);

	// Server names are Latin: shown left to right, cut at their end.
	var sel = h('select', {
		'aria-label': d.name || d.mac,
		dir: d.policy === 'node' ? 'ltr' : null,
		onchange: function(e) {
			var v = e.target.value, body = v.indexOf('node:') === 0 ? { policy: 'node', node: v.slice(5) } : { policy: v };
			busy(e.target, api('PUT', '/api/devices/' + d.mac, body).then(loadTab).catch(function(err) { toast(err.message); loadTab(); }));
		}
	}, options.map(function(o) { return h('option', { value: o[0], selected: value === o[0] }, o[1]); })
		.concat(servers.length ? [h('optgroup', { label: t('server') }, servers.map(function(o) {
			return h('option', { value: o[0], selected: value === o[0] }, o[1]);
		}))] : []));

	var sc = d.schedule;
	return h('div', { 'class': 'fgz-dev', 'data-policy': d.paused ? 'block' : d.policy }, [
		h('span', { 'class': 'state', 'data-on': d.online ? '1' : '0', title: d.online ? '' : t('offlineDev') }),
		h('div', { 'class': 'name' }, present([
			h('b', { dir: 'auto' }, d.name || d.mac),
			h('small', { dir: 'ltr' }, (d.ip ? d.ip + '  ' : '') + d.mac + (d.online ? '' : '  (' + t('offlineDev') + ')')),
			usage,
			sc ? h('small', { 'class': d.paused ? 'paused' : null }, d.paused ? t('pausedNow', sc.to) : t('offHours', sc.from, sc.to)) : null
		])),
		sel,
		h('span', { 'class': 'acts' }, [
			h('button', { 'class': 'fgz-icon', title: t('schedule'), 'aria-label': t('schedule'), 'data-on': sc ? '1' : null,
				onclick: function() { dialogSchedule(d); } }, [icon('clock')]),
			h('button', { 'class': 'fgz-icon', title: t('rename'), 'aria-label': t('rename'), onclick: function() {
				var input = h('input', { type: 'text', value: d.name || '', placeholder: d.hostname || d.mac });
				dialog(t('rename'), d.mac, [h('label', {}, [t('devName'), input])], t('save'), function() {
					return api('PUT', '/api/devices/' + d.mac, { name: input.value }).then(loadTab);
				});
			} }, [icon('pencil')])
		])
	]);
}

// Offline hours. Times are the user's own: the browser's offset from UTC
// goes with them, because the router's clock is usually UTC.
function dialogSchedule(d) {
	var sc = d.schedule || { from: '23:00', to: '07:00' };
	var on = h('input', { type: 'checkbox', 'class': 'fgz-switch', checked: !!d.schedule });
	var from = h('input', { type: 'time', 'class': 'ltr', value: sc.from, required: true });
	var to = h('input', { type: 'time', 'class': 'ltr', value: sc.to, required: true });
	dialog(t('schedule'), t('scheduleHint'), [
		h('label', { 'class': 'fgz-check' }, [on, t('scheduleOn')]),
		h('div', { 'class': 'fgz-two' }, [h('label', {}, [t('from'), from]), h('label', {}, [t('to'), to])])
	], t('save'), function() {
		var body = { schedule: on.checked ? { from: from.value, to: to.value, tz: -new Date().getTimezoneOffset() } : null };
		return api('PUT', '/api/devices/' + d.mac, body).then(loadTab);
	});
}

/* ---------- network: traffic chart and map */

var HIST_MAX = 120; // seconds of traffic the chart keeps

function svgEl(tag, attrs, kids) {
	var el = document.createElementNS('http://www.w3.org/2000/svg', tag);
	for (var k in attrs) if (attrs[k] != null) el.setAttribute(k, attrs[k]);
	[].concat(kids || []).forEach(function(c) {
		if (c != null) el.appendChild(typeof c === 'string' ? document.createTextNode(c) : c);
	});
	return el;
}

// The axis top: four clean steps of 1, 2, 2.5 or 5 times a power of ten.
function niceTop(max) {
	var raw = Math.max(max, 1024) / 4;
	var exp = Math.pow(10, Math.floor(Math.log10(raw)));
	var m = raw / exp;
	var step = m <= 1 ? 1 : m <= 2 ? 2 : m <= 2.5 ? 2.5 : m <= 5 ? 5 : 10;
	return 4 * step * exp;
}

function panelNetwork() {
	var last = S.hist[S.hist.length - 1] || { up: 0, down: 0 };
	var peak = S.hist.reduce(function(a, p) { return { up: Math.max(a.up, p.up), down: Math.max(a.down, p.down) }; }, { up: 0, down: 0 });
	var key = function(cls, label, now, top) {
		return h('span', { 'class': 'fgz-key' }, [h('i', { 'class': cls }), h('span', {}, label),
			h('b', { 'class': 'num', dir: 'ltr', id: 'fgz-now-' + cls }, rate(now)),
			h('small', { 'class': 'num', dir: 'ltr', id: 'fgz-peak-' + cls }, t('peak') + ' ' + rate(top))]);
	};
	var table = h('button', { 'class': 'fgz-btn', onclick: function() { S.showTable = !S.showTable; renderPanel(); } },
		S.showTable ? t('hideTable') : t('showTable'));
	return h('div', {}, [
		section(t('trafficTitle'), [
			h('div', { 'class': 'fgz-bar' }, [key('down', t('download'), last.down, peak.down), key('up', t('upload'), last.up, peak.up),
				h('span', { 'class': 'grow' }), table]),
			h('div', { 'class': 'fgz-chart', id: 'fgz-chart', dir: 'ltr' }),
			S.showTable ? trafficTable() : null
		]),
		section(t('mapTitle'), [h('div', { 'class': 'fgz-map', id: 'fgz-map' }), h('p', { 'class': 'fgz-note' }, t('mapNote'))])
	]);
}

function trafficTable() {
	var rows = S.hist.slice(-20).reverse();
	return h('div', { 'class': 'fgz-table-wrap' }, h('table', { 'class': 'fgz-table' }, [
		h('thead', {}, h('tr', {}, [h('th', {}, t('when')), h('th', {}, t('download')), h('th', {}, t('upload'))])),
		h('tbody', {}, rows.map(function(p) {
			return h('tr', {}, [h('td', {}, t('secAgo', Math.round((Date.now() - p.t) / 1000))),
				h('td', { 'class': 'num', dir: 'ltr' }, rate(p.down)), h('td', { 'class': 'num', dir: 'ltr' }, rate(p.up))]);
		}))
	]));
}

// Two series, one axis (both are bytes per second): download and upload
// over the last two minutes, 2px lines over a 10% wash, end-dots with the
// current value, a crosshair and tooltip that list both series.
function drawTraffic() {
	var box = document.getElementById('fgz-chart');
	if (!box) return;
	var W = Math.max(box.clientWidth, 280), H = 230, padL = 64, padR = 84, padT = 14, padB = 30;
	var data = S.hist, off = HIST_MAX - data.length;
	var max = 0;
	data.forEach(function(p) { max = Math.max(max, p.down, p.up); });
	var top = niceTop(max);
	var x = function(i) { return padL + (W - padL - padR) * (i + off) / (HIST_MAX - 1); };
	var y = function(v) { return padT + (H - padT - padB) * (1 - v / top); };
	var svg = svgEl('svg', { width: W, height: H, viewBox: '0 0 ' + W + ' ' + H, role: 'img', 'aria-label': t('trafficTitle') });
	for (var g = 0; g <= 4; g++) {
		var gy = y(top * g / 4);
		svg.appendChild(svgEl('line', { x1: padL, x2: W - padR, y1: gy, y2: gy, 'class': 'grid' }));
		svg.appendChild(svgEl('text', { x: padL - 8, y: gy + 4, 'text-anchor': 'end', 'class': 'tick' }, g ? rate(top * g / 4) : '0'));
	}
	// A narrow chart keeps only its two ends; the middle label would collide.
	[[0, t('minsAgo', 2)], W < 460 ? null : [60, t('minsAgo', 1)], [HIST_MAX - 1, t('now')]].filter(Boolean).forEach(function(l) {
		svg.appendChild(svgEl('text', { x: padL + (W - padL - padR) * l[0] / (HIST_MAX - 1), y: H - 8,
			'text-anchor': l[0] === 0 ? 'start' : l[0] === HIST_MAX - 1 ? 'end' : 'middle', 'class': 'tick' }, l[1]));
	});
	if (data.length > 1) {
		['up', 'down'].forEach(function(s) {
			var line = data.map(function(p, i) { return (i ? 'L' : 'M') + x(i).toFixed(1) + ' ' + y(p[s]).toFixed(1); }).join('');
			svg.appendChild(svgEl('path', { d: line + 'L' + x(data.length - 1).toFixed(1) + ' ' + y(0) + 'L' + x(0).toFixed(1) + ' ' + y(0) + 'Z', 'class': 'area ' + s }));
			svg.appendChild(svgEl('path', { d: line, 'class': 'line ' + s }));
		});
		// End labels: the current values, nudged apart only by their own
		// height so they stay beside their lines.
		var lp = data[data.length - 1], ex = x(data.length - 1);
		var yd = y(lp.down), yu = y(lp.up);
		if (Math.abs(yd - yu) < 16) { if (yd <= yu) { yd -= 8; yu += 8; } else { yd += 8; yu -= 8; } }
		[['down', lp.down, y(lp.down), yd], ['up', lp.up, y(lp.up), yu]].forEach(function(e) {
			svg.appendChild(svgEl('circle', { cx: ex, cy: e[2], r: 4, 'class': 'dot ' + e[0] }));
			svg.appendChild(svgEl('text', { x: ex + 10, y: e[3] + 4, 'class': 'endlabel' }, rate(e[1])));
		});
	}
	var cross = svgEl('line', { y1: padT, y2: H - padB, 'class': 'cross', visibility: 'hidden' });
	svg.appendChild(cross);
	var hit = svgEl('rect', { x: padL, y: padT, width: W - padL - padR, height: H - padT - padB, fill: 'transparent', tabindex: 0,
		'aria-label': t('trafficTitle') });
	svg.appendChild(hit);
	var tip = h('div', { 'class': 'fgz-tip', hidden: true });
	var show = function(i) {
		if (!data.length) return;
		i = Math.max(0, Math.min(data.length - 1, i));
		S.hoverIdx = i;
		var p = data[i], cx = x(i);
		cross.setAttribute('x1', cx); cross.setAttribute('x2', cx); cross.setAttribute('visibility', 'visible');
		tip.replaceChildren(
			h('div', {}, [h('i', { 'class': 'down' }), h('b', { 'class': 'num' }, rate(p.down)), ' ' + t('download')]),
			h('div', {}, [h('i', { 'class': 'up' }), h('b', { 'class': 'num' }, rate(p.up)), ' ' + t('upload')]),
			h('small', {}, t('secAgo', Math.round((Date.now() - p.t) / 1000))));
		tip.hidden = false;
		tip.style.left = Math.min(cx + 12, W - 170) + 'px';
	};
	hit.addEventListener('pointermove', function(e) {
		var r = svg.getBoundingClientRect();
		show(Math.round((e.clientX - r.left - padL) / (W - padL - padR) * (HIST_MAX - 1)) - off);
	});
	hit.addEventListener('focus', function() { show(data.length - 1); });
	hit.addEventListener('keydown', function(e) {
		if (e.key === 'ArrowLeft' || e.key === 'ArrowRight') { e.preventDefault(); show((S.hoverIdx == null ? data.length - 1 : S.hoverIdx) + (e.key === 'ArrowLeft' ? -1 : 1)); }
	});
	var hide = function() { S.hoverIdx = null; cross.setAttribute('visibility', 'hidden'); tip.hidden = true; };
	hit.addEventListener('pointerleave', hide);
	hit.addEventListener('blur', hide);
	box.replaceChildren(svg, tip);
	if (S.hoverIdx != null) show(S.hoverIdx);
	['down', 'up'].forEach(function(s) {
		var el = document.getElementById('fgz-now-' + s);
		if (el && data.length) el.textContent = rate(data[data.length - 1][s]);
		var pk = document.getElementById('fgz-peak-' + s);
		if (pk) pk.textContent = t('peak') + ' ' + rate(data.reduce(function(m, p) { return Math.max(m, p[s]); }, 0));
	});
}

// The network as a graph: each online device, the router, the server in
// use and the internet. A line's width is that device's speed through the
// tunnel; direct devices take the router's own way out.
function drawMap() {
	var box = document.getElementById('fgz-map');
	if (!box) return;
	var st = S.status || {};
	var up = st.phase === 'connected';
	var devs = (S.devices || []).filter(function(d) { return d.online; });
	var W = Math.max(box.clientWidth, 300), rowH = 48;
	var H = Math.max(230, devs.length * rowH + 40);
	var rtl = lang === 'fa';
	var X = function(f) { return rtl ? W * (1 - f) : W * f; };
	var mid = H / 2;
	var xd = X(W < 520 ? 0.36 : 0.26), xr = X(0.52), xs = X(0.73), xi = X(0.92);
	var rateOf = function(d) { return (d.rate_down || 0) + (d.rate_up || 0); };
	var maxRate = Math.max(1, Math.max.apply(null, devs.map(rateOf).concat([0])));
	var tunnelRate = 0, anyDirect = false;
	devs.forEach(function(d) { if (d.policy === 'direct') anyDirect = true; else if (d.policy !== 'block') tunnelRate += rateOf(d); });
	var width = function(r) { return (1.5 + 6 * Math.min(1, r / maxRate)).toFixed(1); };
	var curve = function(x1, y1, x2, y2) { var c = (x1 + x2) / 2; return 'M' + x1 + ' ' + y1 + 'C' + c + ' ' + y1 + ' ' + c + ' ' + y2 + ' ' + x2 + ' ' + y2; };
	// Coordinates are laid out per direction here, so the SVG itself stays
	// left-to-right: text anchors then mean the same in both languages.
	var svg = svgEl('svg', { width: W, height: H, viewBox: '0 0 ' + W + ' ' + H, role: 'img', 'aria-label': t('mapTitle'), direction: 'ltr' });
	var edge = function(d, cls, w, flowing, title) {
		var g = svgEl('g', {}, [svgEl('title', {}, title), svgEl('path', { d: d, 'class': 'edge ' + cls, 'stroke-width': w })]);
		if (flowing) g.appendChild(svgEl('path', { d: d, 'class': 'flow' }));
		svg.appendChild(g);
	};
	if (anyDirect) {
		var dip = 'M' + xr + ' ' + (mid + 14) + 'C' + xr + ' ' + (H - 14) + ' ' + xi + ' ' + (H - 14) + ' ' + xi + ' ' + (mid + 14);
		edge(dip, 'direct', 1.5, false, t('directPath'));
	}
	edge(curve(xr, mid, xs, mid), up ? 'tunnel' : 'idle', up ? width(tunnelRate) : 1.5, up && tunnelRate > 0, t('router') + ' → ' + (st.current_name || ''));
	edge(curve(xs, mid, xi, mid), up ? 'tunnel' : 'idle', up ? width(tunnelRate) : 1.5, up && tunnelRate > 0, t('internet'));
	devs.forEach(function(d, i) {
		var yy = 20 + rowH * (i + 0.5), r = rateOf(d);
		var cls = d.policy === 'block' ? 'block' : d.policy === 'direct' ? 'direct' : up ? 'tunnel' : 'idle';
		var title = (d.name || d.mac) + ': ' + (d.policy === 'block' ? t('block') : d.policy === 'direct' ? t('direct') : rate(r));
		edge(curve(xd, yy, xr, mid), cls, d.policy === 'block' ? 1.5 : width(r), cls === 'tunnel' && r > 0, title);
		svg.appendChild(svgEl('circle', { cx: xd, cy: yy, r: 5, 'class': 'node dev ' + cls }));
		var anchor = rtl ? 'start' : 'end', tx = rtl ? xd + 12 : xd - 12;
		var name = d.name || d.mac;
		if (name.length > 18) name = name.slice(0, 17) + '…';
		svg.appendChild(svgEl('text', { x: tx, y: yy + (r > 0 ? -1 : 4), 'text-anchor': anchor, 'class': 'lbl' }, name));
		if (r > 0 && cls === 'tunnel')
			svg.appendChild(svgEl('text', { x: tx, y: yy + 14, 'text-anchor': anchor, 'class': 'sub num', direction: 'ltr' }, rate(r)));
		if (d.policy === 'block') {
			var bx = (xd + xr) / 2, by = (yy + mid) / 2;
			svg.appendChild(svgEl('path', { d: 'M' + (bx - 5) + ' ' + (by - 5) + 'l10 10m0 -10l-10 10', 'class': 'cut' }));
		}
	});
	var node = function(xx, label, sub, cls) {
		svg.appendChild(svgEl('circle', { cx: xx, cy: mid, r: 13, 'class': 'node big ' + cls }));
		svg.appendChild(svgEl('text', { x: xx, y: mid + 32, 'text-anchor': 'middle', 'class': 'lbl' }, label));
		if (sub) svg.appendChild(svgEl('text', { x: xx, y: mid + 48, 'text-anchor': 'middle', 'class': 'sub' }, sub));
	};
	var server = st.backup ? 'WARP' : (st.current_name || '');
	if (server.length > 16) server = server.slice(0, 15) + '…';
	node(xr, t('router'), null, 'router');
	node(xs, up ? server : t('disconnected'), up && st.delay ? num(st.delay) + ' ms' : null, up ? 'server' : 'idle');
	node(xi, t('internet'), null, 'net');
	box.replaceChildren(svg);
}

/* ---------- games */

var REGION_FA = { lux: 'لوکزامبورگ', ams: 'آمستردام', fra: 'فرانکفورت', par: 'پاریس', vie: 'وین', sto: 'استکهلم', bah: 'بحرین', dxb: 'دبی' };

function regionName(id) {
	if (lang === 'fa' && REGION_FA[id]) return REGION_FA[id];
	var r = (S.games && S.games.regions || []).filter(function(x) { return x.id === id; })[0];
	return r ? r.name : id;
}

// How a ping feels in a game. Status, so it always comes with its word.
function pingQuality(ms) {
	if (ms < 0) return ['fail', t('failedShort')];
	if (ms < 80) return ['great', t('pingGreat')];
	if (ms < 120) return ['good', t('pingGood')];
	if (ms < 180) return ['ok', t('pingOk')];
	return ['high', t('pingHigh')];
}

// Banner art loads from the publisher's own CDN when the page is viewed;
// nothing is bundled. Without art the banner falls back to its colours.
function gameArt(game) {
	// Steam's own address when FoxGozar has looked it up (newer games keep
	// pictures under hashed paths); the classic one otherwise.
	if (game.art) return game.art;
	if (game.steam) return 'https://shared.akamai.steamstatic.com/store_item_assets/steam/apps/' + game.steam + '/library_600x900_2x.jpg';
	return '';
}

// How a path to a city scores for a game: the 90th percentile carries the
// spikes a player feels; a path that lost more than one round trip in ten
// is pushed behind every steady one.
function gameScore(g, id, region) {
	var m = ((g.results || {})[id] || {})[region];
	if (!(m > 0)) return Infinity;
	var p = ((g.p90 || {})[id] || {})[region];
	return p > 0 ? p : p === -1 ? m + 1000 : m;
}

function gameCity(g, conns, region) {
	var best = null;
	conns.forEach(function(id) {
		var sc = gameScore(g, id, region);
		if (sc < Infinity && (!best || sc < best.score))
			best = { id: id, score: sc, ms: g.results[id][region], jitter: ((g.jitter || {})[id] || {})[region] || 0,
				p90: ((g.p90 || {})[id] || {})[region] };
	});
	return best;
}

function panelGames() {
	var g = S.games || {};
	var res = g.results || {};
	var names = { direct: t('directPath') };
	S.nodes.forEach(function(n) { names[n.id] = n.name; });
	var conns = Object.keys(res).filter(function(id) { return names[id]; });
	var hidden = g.hidden || [];
	var games = (g.games || []).filter(function(x) { return hidden.indexOf(x.id) < 0; });

	var btn = h('button', { 'class': 'fgz-btn primary', onclick: function() {
		if (!S.status || S.status.phase !== 'connected') { toast(t('connectFirst')); return; }
		btn.textContent = t('testing');
		busy(btn, api('POST', '/api/games/test').then(function(r) { S.games = r; renderPanel(); })
			.catch(function(e) { toast(e.message); btn.textContent = t('testGames'); }));
	} }, conns.length ? t('retestGames') : t('testGames'));
	var head = h('div', { 'class': 'fgz-panel fgz-section' }, [
		h('div', { 'class': 'fgz-bar' }, [h('p', { 'class': 'grow fgz-note', style: 'margin:0;padding:0' }, t('gamesNote')), btn]),
		switchField('auto_direct_games', t('autoDirect'), t('autoDirectHint')),
		gameSearch()
	]);

	return h('div', {}, present([
		antiLagCard(), head,
		games.length ? h('div', { 'class': 'fgz-gcards' }, games.map(function(game) { return gameCard(game, g, conns, names); }))
			: h('div', { 'class': 'fgz-panel' }, h('div', { 'class': 'fgz-empty' }, t('noGamesShown')))
	]));
}

// One game, all of it on its card: the picture fills the card, the data
// sits on frosted glass over it. Every card is the same size: the four
// best cities are shown, the rest counted.
var CARD_ROWS = 4;
function gameCard(game, g, conns, names) {
	var cities = game.servers.map(function(sv) { return { sv: sv, c: gameCity(g, conns, sv.region) }; });
	var best = null;
	cities.forEach(function(x) { if (x.c && (!best || x.c.score < best.score)) best = x.c; });
	// Best first; unmeasured or unreachable after, in their own order.
	cities.sort(function(a, b) { return (a.c ? a.c.score : Infinity) - (b.c ? b.c.score : Infinity); });
	var rows = cities.slice(0, CARD_ROWS).map(function(x) {
		var c = x.c, sv = x.sv;
		var q = pingQuality(c ? c.ms : -1);
		return h('div', { 'class': 'fgz-grow', 'data-q': conns.length ? q[0] : 'none' }, [
			h('span', { 'class': 'dot' }),
			h('div', { 'class': 'where' }, [h('b', { dir: 'auto' }, sv.name),
				h('small', { dir: 'auto' }, c ? names[c.id] : conns.length ? t('unreachable') : t('notMeasured'))]),
			h('div', { 'class': 'nums' }, [
				h('b', { 'class': 'ms num', dir: 'ltr' }, c ? num(c.ms) + ' ms' : '—'),
				h('small', { 'class': 'num', dir: 'ltr', title: c && c.p90 > 0 && c.p90 - c.ms >= 30 ? t('p90Hint') : t('jitterHint') },
					c ? (c.p90 > 0 && c.p90 - c.ms >= 30 ? t('spikesTo', num(c.p90)) : '±' + num(c.jitter)) : '')
			])
		]);
	});
	var q = pingQuality(best ? best.ms : -1);
	var art = gameArt(game);
	var img = art ? h('img', { 'class': 'bg', src: art, alt: '', loading: 'lazy', decoding: 'async', referrerpolicy: 'no-referrer' }) : null;
	if (img) img.addEventListener('error', function() { img.remove(); });
	var bin = h('button', { 'class': 'fgz-icon bin', title: t('removeGame'), 'aria-label': t('removeGame'), onclick: function(e) {
		var b = e.currentTarget;
		if (!confirmInline(b, t('removeGameAsk', game.name))) return;
		busy(b, api('POST', '/api/games/' + game.id + '/remove').then(function(r) { S.games = r; renderPanel(); })
			.catch(function(err) { toast(err.message); }));
	} }, [icon('trash')]);
	var routable = (g.routable || {})[game.id];
	var more = cities.length - CARD_ROWS;
	return h('article', { 'class': 'fgz-gcard', 'data-game': game.id, 'data-q': conns.length ? q[0] : 'none' }, present([
		img,
		h('div', { 'class': 'shade' }),
		h('div', { 'class': 'content' }, present([
			h('div', { 'class': 'top' }, present([
				conns.length ? h('span', { 'class': 'ping num', dir: 'ltr' }, best ? num(best.ms) + ' ms' : '—') : h('span'),
				bin
			])),
			h('h4', { dir: 'ltr' }, game.name),
			h('div', { 'class': 'glass' }, present([
				h('div', { 'class': 'rows' }, rows),
				more > 0 ? h('small', { 'class': 'more' }, t('moreCities', more)) : null
			])),
			h('div', { 'class': 'gfoot' }, routable ? [
				gameRouteBar(game, g, conns, names, (g.routes || {})[game.id] || ''),
				directCheck(game)
			] : [h('small', { 'class': 'note' }, game.id.indexOf('my') === 0 ? t('customGameHint') : t('notRoutable'))])
		]))
	]));
}

// Is the game's own server reachable without the tunnel, and faster? Some
// games block their launcher in Iran but not their servers.
function directCheck(game) {
	// The one asked for now, or the automatic one from after the last match.
	var r = (S.directCheck || {})[game.id] || ((S.games || {}).checks || {})[game.id];
	var line = null;
	if (r) {
		var text = r.advice === 'play-first' ? t('playFirst')
			: r.advice === 'unknown' ? t('noPingAnswer')
			: r.advice === 'direct' ? t('directBetter', num(r.best_direct), r.best_tunnel ? num(r.best_tunnel) : '—')
			: t('tunnelBetter', num(r.best_direct), num(r.best_tunnel));
		line = h('div', { 'class': 'fgz-dcheck', 'data-advice': r.advice }, present([
			h('small', {}, text),
			r.advice === 'direct' && ((S.games.routes || {})[game.id] !== 'direct') ? h('button', { 'class': 'fgz-btn primary', onclick: function(e) {
				busy(e.currentTarget, api('PUT', '/api/games/' + game.id + '/route', { via: 'direct' })
					.then(function(res) { S.games = res; toast(t('saved')); renderPanel(); }).catch(function(err) { toast(err.message); }));
			} }, t('useDirect')) : null
		]));
	}
	return h('div', { 'class': 'fgz-dcheckwrap' }, present([
		h('button', { 'class': 'fgz-btn', title: t('checkDirectHint'), onclick: function(e) {
			busy(e.currentTarget, api('POST', '/api/games/' + game.id + '/check').then(function(res) {
				S.directCheck = S.directCheck || {};
				S.directCheck[game.id] = res;
				renderPanel();
			}).catch(function(err) { toast(err.message); }));
		} }, t('checkDirect')),
		line
	]));
}

// Search as you type: games already known (hidden ones can come back) and
// Steam's store. A result is added with one press.
function gameSearch() {
	var list = h('div', { 'class': 'fgz-gsearch-list', role: 'listbox' });
	var timer = null, seq = 0;
	var show = function(hits, q) {
		list.replaceChildren.apply(list, hits.length ? hits.map(function(hit) {
			var state = hit.shown ? t('inList') : hit.hidden ? t('restoreGame') : t('addThis');
			return h('button', { type: 'button', 'class': 'fgz-ghit', role: 'option', disabled: hit.shown, onclick: function(e) {
				busy(e.currentTarget, api('POST', '/api/games/add', hit).then(function(r) {
					S.games = r; input.value = ''; list.replaceChildren(); toast(t('gameAdded', hit.name)); renderPanel();
				}).catch(function(err) { toast(err.message); }));
			} }, present([
				hit.image ? h('img', { src: hit.image, alt: '', loading: 'lazy', referrerpolicy: 'no-referrer' }) : h('span', { 'class': 'noimg' }),
				h('span', { 'class': 'n', dir: 'ltr' }, hit.name),
				h('small', {}, state)
			]));
		}) : [h('small', { 'class': 'fgz-help', style: 'padding:8px 4px' }, q.length < 2 ? '' : t('noGameFound'))]);
	};
	var input = h('input', { type: 'search', dir: 'auto', placeholder: t('searchGamesPh'), 'aria-label': t('searchGamesPh'),
		oninput: function() {
			clearTimeout(timer);
			var q = input.value.trim(), my = ++seq;
			if (q.length < 2) { list.replaceChildren(); return; }
			list.replaceChildren(h('small', { 'class': 'fgz-help', style: 'padding:8px 4px' }, t('searching')));
			timer = setTimeout(function() {
				api('GET', '/api/games/search?q=' + encodeURIComponent(q)).then(function(hits) {
					if (my === seq) show(hits || [], q); // only the latest answer
				}).catch(function(err) { if (my === seq) list.replaceChildren(h('small', { 'class': 'fgz-help' }, err.message)); });
			}, 400);
		} });
	return h('div', { 'class': 'fgz-gsearch' }, [input, list]);
}

// Anti-lag (SQM/cake): the line's speeds, a switch, and what it is doing.
function antiLagCard() {
	var l = S.lag;
	if (!l) return h('div');
	var down = h('input', { type: 'number', min: '1', 'class': 'ltr', value: l.down_mbps || '', placeholder: 'Mbps' });
	var up = h('input', { type: 'number', min: '1', 'class': 'ltr', value: l.up_mbps || '', placeholder: 'Mbps' });
	var put = function(on, btn) {
		var p = api('PUT', '/api/antilag', { on: on, down_mbps: parseInt(down.value, 10) || 0, up_mbps: parseInt(up.value, 10) || 0 })
			.then(function(r) { S.lag = r; renderPanel(); }).catch(function(err) { toast(err.message); renderPanel(); });
		if (btn) busy(btn, p);
		return p;
	};
	var status = l.installing ? t('lagInstalling') : l.error ? l.error : l.on ? (l.active ? t('lagActive') : t('lagStarting')) : null;
	return h('div', { 'class': 'fgz-section' }, [h('h3', {}, t('antiLag')), h('div', { 'class': 'fgz-panel' }, present([
		h('div', { 'class': 'fgz-field' }, [
			h('label', { 'for': 'fgz-lag' }, [t('antiLagLabel'), h('small', {}, t('antiLagHint'))]),
			h('input', { id: 'fgz-lag', type: 'checkbox', 'class': 'fgz-switch', checked: !!l.on,
				onchange: function(e) { put(e.target.checked); } })
		]),
		h('div', { 'class': 'fgz-field' }, [
			h('label', {}, [t('lineSpeed'), h('small', {}, t('lineSpeedHint'))]),
			h('div', { style: 'display:grid;gap:8px' }, [
				h('div', { 'class': 'fgz-two' }, [h('label', {}, [t('download'), down]), h('label', {}, [t('upload'), up])]),
				h('button', { 'class': 'fgz-btn', onclick: function(e) { put(!!l.on, e.currentTarget).then(function() { toast(t('saved')); }); } }, t('save'))
			])
		]),
		status ? h('p', { 'class': 'fgz-help', style: 'padding-top:12px' }, [h('span', { 'class': 'fgz-statusline', 'data-on': l.active ? '1' : '0' }, status)]) : null
	]))]);
}

// The path this game's match traffic takes, with the best one from the
// last test pointed out (steadiest low ping to any of the game's cities).
function gameRouteBar(game, g, conns, names, cur) {
	var best = null;
	game.servers.forEach(function(sv) {
		var c = gameCity(g, conns, sv.region);
		if (c && (!best || c.score < best.score)) best = c;
	});
	var opts = [['', t('gameViaTunnel')], ['direct', t('directPath')]].concat(S.nodes.map(function(n) { return [n.id, n.name]; }));
	var sel = h('select', { 'aria-label': t('gamePath'), onchange: function(e) {
		busy(e.target, api('PUT', '/api/games/' + game.id + '/route', { via: e.target.value })
			.then(function(r) { S.games = r; toast(t('saved')); renderPanel(); })
			.catch(function(err) { toast(err.message); renderPanel(); }));
	} }, opts.map(function(o) {
		var mark = best && best.id === o[0] ? '  ★ ' + num(best.ms) + ' ms' : '';
		return h('option', { value: o[0], selected: o[0] === cur }, o[1] + mark);
	}));
	return h('div', { 'class': 'fgz-route' }, present([
		h('label', {}, t('gamePath')),
		sel,
		cur === 'direct' ? h('small', {}, t('directGameHint')) : best ? h('small', {}, t('gameBest', names[best.id], num(best.ms))) : null
	]));
}

/* ---------- subscriptions */

function panelSubs() {
	var bar = h('div', { 'class': 'fgz-bar' }, [h('span', { 'class': 'grow' }), h('button', { 'class': 'fgz-btn primary', onclick: dialogSub }, t('addSub'))]);
	if (!S.subs.length)
		return h('div', { 'class': 'fgz-panel' }, [bar, h('div', { 'class': 'fgz-empty' }, [h('b', {}, t('noSubs')), t('noSubsHint')])]);
	return h('div', { 'class': 'fgz-panel' }, [bar].concat(S.subs.map(function(s) {
		var used = (s.upload || 0) + (s.download || 0);
		var meta = [h('span', {}, t('servers', s.node_count || 0))];
		if (s.total) meta.push(h('span', { 'class': 'num' }, t('of', bytes(used), bytes(s.total))));
		if (s.expire) {
			var days = Math.floor((s.expire * 1000 - Date.now()) / 864e5);
			meta.push(h('span', {}, days < 0 ? t('expired') : t('expires', days)));
		}
		if (s.updated_at) meta.push(h('span', {}, t('lastUpdate', ago(s.updated_at))));
		if (s.interval) meta.push(h('span', {}, t('every', s.interval)));
		return h('div', { 'class': 'fgz-sub' }, [
			h('div', { 'class': 'fgz-sub-top' }, [
				h('b', {}, s.name),
				h('button', { 'class': 'fgz-icon', title: t('update'), 'aria-label': t('update'), onclick: function(e) {
					busy(e.currentTarget, api('POST', '/api/subs/' + s.id + '/update').then(function() { toast(t('updated')); return loadTab(); })
						.catch(function(err) { toast(err.message); return loadTab(); }));
				} }, [icon('refresh')]),
				h('button', { 'class': 'fgz-icon', title: t('remove'), 'aria-label': t('remove'), onclick: function() {
					api('DELETE', '/api/subs/' + s.id).then(loadTab).catch(function(err) { toast(err.message); });
				} }, [icon('trash')])
			]),
			s.total ? h('div', { 'class': 'fgz-meter' }, [h('i', { style: 'width:' + Math.min(100, used / s.total * 100).toFixed(1) + '%' })]) : null,
			h('div', { 'class': 'fgz-sub-meta' }, meta),
			s.last_error ? h('div', { 'class': 'fgz-sub-err' }, s.last_error) : null
		]);
	})));
}

function dialogSub() {
	var url = h('input', { type: 'url', 'class': 'ltr', required: true, placeholder: 'https://…' });
	var name = h('input', { type: 'text', placeholder: t('subName') });
	dialog(t('addSub'), null, [h('label', {}, [t('subUrl'), url]), h('label', {}, [t('subName'), name])], t('addSub'), function() {
		return api('POST', '/api/subs', { url: url.value, name: name.value }).then(function(s) {
			toast(t('servers', (s && s.node_count) || 0));
			return loadTab();
		});
	});
}

/* ---------- routing */

// The site doctor: a site that does not open, tried directly and through
// every server, with the rule that fixes it one press away.
function siteDoctor() {
	var input = h('input', { type: 'search', dir: 'auto', placeholder: t('doctorPh'), 'aria-label': t('doctorPh'),
		value: S.doctorSite || '', onkeydown: function(e) { if (e.key === 'Enter') { e.preventDefault(); go.click(); } } });
	var go = h('button', { 'class': 'fgz-btn primary', onclick: function() {
		if (!input.value.trim()) { input.focus(); return; }
		if (!S.status || S.status.phase !== 'connected') { toast(t('connectFirst')); return; }
		S.doctorSite = input.value;
		go.textContent = t('testing');
		busy(go, api('POST', '/api/doctor', { site: input.value }).then(function(r) { S.doctor = r; renderPanel(); })
			.catch(function(err) { toast(err.message); go.textContent = t('doctorGo'); }));
	} }, t('doctorGo'));
	var r = S.doctor;
	var kids = [h('div', { 'class': 'fgz-bar' }, [h('div', { 'class': 'grow', style: 'min-width:0' }, input), go])];
	if (r) {
		kids.push(h('div', { 'class': 'fgz-doctor' }, r.paths.map(function(p) {
			var what = p.ok ? (p.detail === 'challenge' ? t('drChallenge') : t('drOpens'))
				: p.detail === 'sanctioned' ? t('drSanctioned')
				: p.problem === 'filtered' ? t('drFilteredDns')
				: p.problem === 'refused' ? (p.detail === 'country' ? t('drCountry') : t('drRefused', p.status))
				: (p.problem === 'reset' || p.problem === 'timeout') && p.detail === 'sni' ? t('drFilteredSni')
				: p.problem === 'timeout' ? t('drTimeout') : p.problem === 'tls' ? t('drTls') : p.problem === 'dns' ? t('drDns')
				: (p.detail || t('drError'));
			return h('div', { 'class': 'fgz-drow', 'data-ok': p.ok ? '1' : '0' }, [
				h('span', { 'class': 'dot' }),
				h('b', { dir: 'auto' }, p.path === 'direct' ? t('directPath') : p.name),
				h('small', {}, what),
				h('small', { 'class': 'num', dir: 'ltr' }, p.ok && p.ms ? num(p.ms) + ' ms' : '')
			]);
		})));
		var target = r.suggest, label = target === 'direct' ? t('directPath') : target === 'proxy' ? t('toProxy')
			: (S.nodes.filter(function(n) { return n.id === target; })[0] || {}).name;
		var why = r.why === 'direct-works' ? t('drWhyDirect', r.domain) : r.why === 'only-tunnel' ? t('drWhyTunnel', r.domain)
			: r.why === 'only-some' ? t('drWhySome', r.domain, label) : t('drWhyNothing', r.domain);
		kids.push(h('div', { 'class': 'fgz-dsuggest', 'data-why': r.why }, present([
			h('p', {}, present([why, r.sanctioned ? ' ' + t('drWhySanctioned') : null])),
			// No rule means the tunnel already: a rule saying so would change nothing.
			target && r.current !== target && !(target === 'proxy' && !r.current) ? h('button', { 'class': 'fgz-btn primary', onclick: function(e) {
				busy(e.currentTarget, api('POST', '/api/doctor/apply', { site: r.domain, target: target }).then(function() {
					r.current = target; toast(t('drApplied', r.domain, label));
					return api('GET', '/api/rules').then(function(x) { S.rules = x; renderPanel(); });
				}).catch(function(err) { toast(err.message); }));
			} }, t('drApply', label)) : target ? h('small', {}, t('drAlready')) : null
		])));
	}
	return h('div', { 'class': 'fgz-section' }, [h('h3', {}, t('doctor')), h('div', { 'class': 'fgz-panel' }, [
		h('p', { 'class': 'fgz-help', style: 'padding:14px 18px 0' }, t('doctorHint'))
	].concat(kids))]);
}

function panelRules() {
	var s = S.settings || {};
	var toggles = h('div', { 'class': 'fgz-panel fgz-section' }, [
		switchField('iran_direct', t('iranDirect'), t('iranDirectHint')),
		switchField('block_ads', t('blockAds'), t('blockAdsHint')),
		switchField('block_malware', t('blockMalware'), t('blockMalwareHint')),
		switchField('quic_block', t('quicBlock'), t('quicBlockHint')),
		switchField('game_downloads_direct', t('gameDl'), t('gameDlHint'))
	]);

	var targets = [['direct', t('toDirect')], ['proxy', t('toProxy')], ['block', t('toBlock')]]
		.concat(S.nodes.map(function(n) { return [n.id, n.name]; }));

	var list = S.rules.map(function(r, i) {
		var text = (r.domains || []).concat((r.keywords || []).map(function(k) { return 'keyword:' + k; }), r.ips || [], r.urls || []).join(', ');
		return h('div', { 'class': 'fgz-rule' }, [
			h('input', { type: 'checkbox', 'class': 'fgz-switch', checked: r.enabled, 'aria-label': t('customRules'),
				onchange: function(e) { r.enabled = e.target.checked; } }),
			h('input', { type: 'text', 'class': 'ltr', value: text, placeholder: t('rulePlaceholder'),
				onchange: function(e) {
					// domain, 1.2.3.0/24, keyword:word, or a link to a list
					var parts = e.target.value.split(/[,\s]+/).filter(Boolean);
					r.urls = parts.filter(function(p) { return /^https?:\/\//i.test(p); });
					r.keywords = parts.filter(function(p) { return /^keyword:./i.test(p); }).map(function(p) { return p.slice(8); });
					r.ips = parts.filter(function(p) { return /^[\d.:a-f]+\/\d+$|^\d+\.\d+\.\d+\.\d+$/i.test(p); });
					r.domains = parts.filter(function(p) { return r.ips.indexOf(p) < 0 && r.urls.indexOf(p) < 0 && !/^keyword:/i.test(p); });
				} }),
			h('select', { dir: ['direct', 'proxy', 'block'].indexOf(r.target) < 0 ? 'ltr' : null,
				onchange: function(e) { r.target = e.target.value; e.target.dir = ['direct', 'proxy', 'block'].indexOf(r.target) < 0 ? 'ltr' : ''; } }, targets.map(function(o) {
				return h('option', { value: o[0], selected: r.target === o[0] }, o[1]);
			})),
			h('button', { 'class': 'fgz-icon', title: t('remove'), 'aria-label': t('remove'), onclick: function() { S.rules.splice(i, 1); renderPanel(); } }, [icon('trash')])
		]);
	});

	var custom = h('div', { 'class': 'fgz-panel' }, [
		h('div', { 'class': 'fgz-bar' }, [
			h('b', { 'class': 'grow' }, t('customRules')),
			h('button', { 'class': 'fgz-btn', onclick: function() {
				S.rules.push({ enabled: true, domains: [], ips: [], target: 'direct' }); renderPanel();
				var inputs = panelEl.querySelectorAll('.fgz-rule input[type=text]'); inputs[inputs.length - 1].focus();
			} }, t('addRule')),
			h('button', { 'class': 'fgz-btn primary', onclick: function(e) {
				busy(e.currentTarget, api('PUT', '/api/rules', S.rules).then(function(r) { S.rules = r; toast(t('saved')); renderPanel(); })
					.catch(function(err) { toast(err.message); }));
			} }, t('save'))
		]),
		h('p', { 'class': 'fgz-help' }, t('rulesHelp'))
	].concat(list.length ? list : [h('div', { 'class': 'fgz-empty' }, t('noRules'))]));

	return h('div', {}, [siteDoctor(), toggles, custom]);
}

/* ---------- settings */

function saveSetting(patch) {
	return api('PUT', '/api/settings', patch).then(function(s) { S.settings = s; toast(t('saved')); })
		.catch(function(e) { toast(e.message); });
}

function switchField(key, label, hint) {
	var id = 'fgz-' + key;
	return h('div', { 'class': 'fgz-field' }, [
		h('label', { 'for': id }, [label, hint ? h('small', {}, hint) : null]),
		h('input', { id: id, type: 'checkbox', 'class': 'fgz-switch', checked: !!(S.settings && S.settings[key]),
			onchange: function(e) { var p = {}; p[key] = e.target.checked; saveSetting(p); } })
	]);
}

function inputField(key, label, hint, type, toValue, fromValue) {
	var id = 'fgz-' + key;
	var v = S.settings ? S.settings[key] : '';
	return h('div', { 'class': 'fgz-field' }, [
		h('label', { 'for': id }, [label, hint ? h('small', {}, hint) : null]),
		h('input', { id: id, type: type || 'text', 'class': 'ltr', value: fromValue ? fromValue(v) : (v == null ? '' : v),
			onchange: function(e) { var p = {}; p[key] = toValue ? toValue(e.target.value) : e.target.value; saveSetting(p); } })
	]);
}

function selectField(key, label, options) {
	var id = 'fgz-' + key;
	return h('div', { 'class': 'fgz-field' }, [
		h('label', { 'for': id }, label),
		h('select', { id: id, onchange: function(e) { var p = {}; p[key] = e.target.value; saveSetting(p); } }, options.map(function(o) {
			return h('option', { value: o, selected: S.settings && S.settings[key] === o }, o);
		}))
	]);
}

// The address servers are measured with: a site that matters to the user
// makes automatic choice skip servers that cannot open it.
var TEST_TARGETS = [
	['https://cp.cloudflare.com/generate_204', 'Cloudflare'],
	['https://www.youtube.com/generate_204', 'YouTube'],
	['https://www.instagram.com/robots.txt', 'Instagram'],
	['https://www.google.com/generate_204', 'Google'],
	['https://telegram.org/robots.txt', 'Telegram']
];

function testTargetField() {
	var cur = S.settings.test_url;
	var known = TEST_TARGETS.some(function(x) { return x[0] === cur; });
	var custom = h('input', { type: 'url', 'class': 'ltr', value: cur, hidden: known,
		onchange: function(e) { saveSetting({ test_url: e.target.value }); } });
	var sel = h('select', { id: 'fgz-test-url', onchange: function(e) {
		if (e.target.value === '') { custom.hidden = false; custom.focus(); return; }
		custom.hidden = true;
		saveSetting({ test_url: e.target.value });
	} }, TEST_TARGETS.map(function(x) { return h('option', { value: x[0], selected: x[0] === cur }, x[1]); })
		.concat([h('option', { value: '', selected: !known }, t('custom'))]));
	return h('div', { 'class': 'fgz-field' }, [
		h('label', { 'for': 'fgz-test-url' }, [t('testTarget'), h('small', {}, t('testTargetHint'))]),
		h('div', { style: 'display:grid;gap:6px' }, [sel, custom])
	]);
}

function section(title, kids) {
	return h('div', { 'class': 'fgz-section' }, [h('h3', {}, title), h('div', { 'class': 'fgz-panel' }, kids)]);
}

function panelSettings() {
	if (!S.settings) return h('div');
	var int = function(v) { return parseInt(v, 10) || 0; };
	var list = function(v) { return v.split(/[,\s]+/).filter(Boolean); };
	var joined = function(v) { return (v || []).join(', '); };
	var coreRow = function(kind, label) {
		var v = S.cores[kind === 'sing-box' ? 'sing_box' : kind];
		return h('div', { 'class': 'fgz-field' }, [
			h('label', {}, [label, h('small', { 'class': 'num' }, v ? t('installed', v) : t('notInstalled'))]),
			h('button', { 'class': 'fgz-btn', onclick: function(e) {
				var b = e.currentTarget; b.textContent = t('updating');
				busy(b, api('POST', '/api/core/' + kind + '/update').then(function(r) {
					S.cores[kind === 'sing-box' ? 'sing_box' : kind] = r.version; toast(t('updated')); renderPanel();
				}).catch(function(err) { toast(err.message); renderPanel(); }));
			} }, t('updateCore', label))
		]);
	};
	return h('div', {}, [
		section(t('sConnection'), [
			testTargetField(),
			inputField('test_interval', t('testInterval'), null, 'number', int),
			inputField('tolerance', t('tolerance'), t('toleranceHint'), 'number', int),
			switchField('keep_warm', t('keepWarm'), t('keepWarmHint')),
			switchField('auto_rescue', t('autoRescue'), t('autoRescueHint')),
			switchField('smart_auto', t('smartAuto'), t('smartAutoHint'))
		]),
		section(t('sDns'), [
			inputField('remote_dns', t('remoteDns'), 'https://1.1.1.1/dns-query'),
			inputField('direct_dns', t('directDns'), t('directDnsHint')),
			switchField('fake_ip', t('fakeIp'), t('fakeIpHint'))
		]),
		section(t('sNetwork'), [
			switchField('proxy_router', t('proxyRouter')),
			switchField('kill_switch', t('killSwitch'), t('killSwitchHint')),
			switchField('ipv6', t('ipv6')),
			inputField('lan_interfaces', t('lan'), t('lanHint'), 'text', list, joined)
		]),
		section(t('sWarp'), [
			switchField('warp_backup', t('warpBackup'), t('warpBackupHint')),
			h('div', { 'class': 'fgz-field', id: 'fgz-warp' }, warpLine())
		]),
		section(t('sAdvanced'), [
			switchField('mux', t('mux'), t('muxHint')),
			switchField('tls_fragment', t('fragment'), t('fragmentHint')),
			selectField('log_level', t('logLevel'), ['error', 'warn', 'info', 'debug']),
			inputField('github_mirror', t('mirror'), t('mirrorHint'), 'url')
		]),
		section(t('sBackup'), [
			h('div', { 'class': 'fgz-field' }, [
				h('label', {}, [t('backup'), h('small', {}, t('backupHint'))]),
				h('a', { 'class': 'fgz-btn', href: BASE + '/api/backup?sid=' + encodeURIComponent(SID), download: 'foxgozar-backup.json' }, t('download'))
			]),
			h('div', { 'class': 'fgz-field' }, [
				h('label', { 'for': 'fgz-restore' }, [t('restore'), h('small', {}, t('restoreHint'))]),
				h('label', { 'class': 'fgz-btn' }, [t('chooseFile'), h('input', { id: 'fgz-restore', type: 'file', accept: '.json,application/json', hidden: true,
					onchange: function(e) {
						var f = e.target.files[0];
						if (!f) return;
						f.text().then(function(txt) {
							var body;
							try { body = JSON.parse(txt); } catch (x) { throw new Error(t('notBackup')); }
							return api('POST', '/api/restore', body);
						}).then(function(r) {
							toast(t('restored', r.nodes, r.subscriptions, r.devices));
							return api('GET', '/api/settings').then(function(s) { S.settings = s; loadTab(); });
						}).catch(function(err) { toast(err.message); e.target.value = ''; });
					} })])
			])
		]),
		section(t('sCores'), [
			coreRow('sing-box', 'sing-box'),
			coreRow('xray', 'Xray'),
			h('div', { 'class': 'fgz-field' }, [
				h('label', {}, t('coreBeta')),
				h('input', { type: 'checkbox', 'class': 'fgz-switch', checked: S.settings.core_channel === 'beta',
					onchange: function(e) { saveSetting({ core_channel: e.target.checked ? 'beta' : 'stable' }); } })
			]),
			h('div', { 'class': 'fgz-field' }, [
				h('label', {}, t('updateRules')),
				h('button', { 'class': 'fgz-btn', onclick: function(e) {
					busy(e.currentTarget, api('POST', '/api/rulesets/update').then(function() { toast(t('updated')); })
						.catch(function(err) { toast(err.message); }));
				} }, t('update'))
			])
		])
	]);
}

// Sharing this router's internet with people on the domestic network: for
// a shutdown, with Starlink (or any second way out) on the router.
function sharingFields() {
	var sh = S.sharing;
	if (!sh) return [];
	var put = function(body, btn) {
		var p = api('PUT', '/api/sharing', body).then(function(r) { S.sharing = r; renderPanel(); })
			.catch(function(err) { toast(err.message); renderPanel(); });
		if (btn) busy(btn, p);
		return p;
	};
	var cur = function() { return { enabled: sh.enabled, port: sh.port, sni: sh.sni }; };
	var status = !sh.enabled ? null
		: !sh.users.some(function(u) { return u.enabled; }) ? t('sharingNoUsers')
		: !sh.running ? t('sharingDown')
		: sh.address ? t('sharingAt', sh.address + ':' + sh.port, sh.via) : t('sharingNoAddr');
	var fields = [
		h('div', { 'class': 'fgz-field' }, [
			h('label', { 'for': 'fgz-sharing' }, [t('sharing'), h('small', {}, t('sharingHint'))]),
			h('input', { id: 'fgz-sharing', type: 'checkbox', 'class': 'fgz-switch', checked: !!sh.enabled,
				onchange: function(e) { var b = cur(); b.enabled = e.target.checked; put(b); } })
		]),
		h('div', { 'class': 'fgz-field' }, [
			h('label', { 'for': 'fgz-sharing-port' }, [t('sharingPort'), h('small', {}, t('sharingPortHint'))]),
			h('input', { id: 'fgz-sharing-port', type: 'number', 'class': 'ltr', value: sh.port,
				onchange: function(e) { var b = cur(); b.port = parseInt(e.target.value, 10) || 0; put(b); } })
		]),
		h('div', { 'class': 'fgz-field' }, [
			h('label', { 'for': 'fgz-sharing-sni' }, [t('sharingSni'), h('small', {}, t('sharingSniHint'))]),
			h('input', { id: 'fgz-sharing-sni', type: 'text', 'class': 'ltr', value: sh.sni,
				onchange: function(e) { var b = cur(); b.sni = e.target.value; put(b); } })
		])
	];
	if (status)
		fields.push(h('div', { 'class': 'fgz-field' }, [
			h('label', {}, [sh.running ? t('sharingOn') : t('sharingOff'), h('small', { 'class': 'num' }, status)]),
			h('button', { 'class': 'fgz-btn', title: t('newKeyHint'), onclick: function(e) {
				var b = cur(); b.new_key = true;
				put(b, e.currentTarget).then(function() { toast(t('newKeyDone')); });
			} }, t('newKey'))
		]));
	fields.push(h('p', { 'class': 'fgz-help', style: 'padding-top:12px' }, [
		sh.dualwan ? t('dualwanOn') : t('dualwanOff'), ' ',
		h('code', { dir: 'ltr' }, sh.dualwan ? 'foxgozar dualwan off --yes' : 'foxgozar dualwan on --yes')
	]));
	return fields;
}

/* ---------- sharing: one account per person */

function panelShare() {
	var sh = S.sharing;
	if (!sh) return h('div');
	var users = sh.users || [];
	var list = h('div', { 'class': 'fgz-panel' }, [
		h('div', { 'class': 'fgz-bar' }, [
			h('b', { 'class': 'grow' }, t('accounts', users.length)),
			h('button', { 'class': 'fgz-btn primary', onclick: dialogAccount }, t('addAccount'))
		]),
		h('p', { 'class': 'fgz-help' }, t('accountsHint'))
	].concat(users.length ? users.map(accountRow) : [h('div', { 'class': 'fgz-empty' }, [h('b', {}, t('noAccounts')), t('noAccountsHint')])]));
	return h('div', {}, [
		section(t('sSharing'), sharingFields()),
		h('div', { 'class': 'fgz-section' }, [list])
	]);
}

function accountRow(u) {
	var used = (u.up || 0) + (u.down || 0);
	var meta = [h('span', { 'class': 'num', dir: 'ltr' }, '↓ ' + bytes(u.down) + '  ↑ ' + bytes(u.up))];
	if (u.limit) meta.push(h('span', { 'class': 'num' }, t('of', bytes(used), bytes(u.limit))));
	if (u.online) meta.push(h('span', { 'class': 'num', dir: 'ltr' }, rate(u.rate_down + u.rate_up)));
	meta.push(h('span', {}, u.online ? t('onlineNow', u.connections) : u.last_seen ? t('lastSeen', ago(u.last_seen)) : t('neverSeen')));
	var edit = function(body) {
		return api('PUT', '/api/sharing/users/' + u.id, body).then(function(r) { S.sharing = r; renderPanel(); })
			.catch(function(err) { toast(err.message); });
	};
	return h('div', { 'class': 'fgz-acct', 'data-on': u.enabled ? '1' : '0' }, [
		h('span', { 'class': 'state', 'data-on': u.online ? '1' : '0' }),
		h('div', { 'class': 'name' }, present([
			h('b', { dir: 'auto' }, u.name),
			u.limit ? h('div', { 'class': 'fgz-meter' }, [h('i', { style: 'width:' + Math.min(100, used / u.limit * 100).toFixed(1) + '%' })]) : null,
			h('div', { 'class': 'fgz-sub-meta' }, meta),
			u.enabled ? null : h('small', { 'class': 'paused' }, u.limit && used >= u.limit ? t('pausedLimit') : t('pausedByYou'))
		])),
		h('span', { 'class': 'acts' }, [
			h('button', { 'class': 'fgz-icon', title: t('showLink'), 'aria-label': t('showLink'), onclick: function() { accountLink(u); } }, [icon('qr')]),
			h('input', { type: 'checkbox', 'class': 'fgz-switch', checked: u.enabled, title: t('accountOn'), 'aria-label': t('accountOn'),
				onchange: function(e) { edit({ enabled: e.target.checked }); } }),
			h('button', { 'class': 'fgz-icon', title: t('rename'), 'aria-label': t('rename'), onclick: function() { dialogAccount(u); } }, [icon('pencil')]),
			h('button', { 'class': 'fgz-icon', title: t('remove'), 'aria-label': t('remove'), onclick: function() {
				if (!confirmInline(this, t('removeAccount', u.name))) return;
				api('DELETE', '/api/sharing/users/' + u.id).then(function(r) { S.sharing = r; renderPanel(); }).catch(function(err) { toast(err.message); });
			} }, [icon('trash')])
		])
	]);
}

// A destructive button asks once by changing its title, then acts on the
// second press (no browser dialogs: they freeze LuCI on some phones).
function confirmInline(btn, msg) {
	if (btn.getAttribute('data-armed')) return true;
	btn.setAttribute('data-armed', '1');
	btn.classList.add('armed');
	toast(msg);
	setTimeout(function() { btn.removeAttribute('data-armed'); btn.classList.remove('armed'); }, 4000);
	return false;
}

// New account (no argument) or changes to one.
function dialogAccount(u) {
	var isNew = !u || !u.id;
	var name = h('input', { type: 'text', required: true, value: isNew ? '' : u.name, placeholder: t('accountNamePh') });
	var limit = h('input', { type: 'number', min: '0', step: '1', 'class': 'ltr', value: !isNew && u.limit ? Math.round(u.limit / 1e9) : '', placeholder: t('noLimit') });
	var fields = [h('label', {}, [t('accountName'), name]), h('label', {}, [t('limitGB'), limit])];
	var reset, newLink;
	if (!isNew) {
		reset = h('input', { type: 'checkbox', 'class': 'fgz-switch' });
		newLink = h('input', { type: 'checkbox', 'class': 'fgz-switch' });
		fields.push(h('label', { 'class': 'fgz-check' }, [reset, t('resetUsage')]));
		fields.push(h('label', { 'class': 'fgz-check' }, [newLink, t('newLink')]));
	}
	dialog(isNew ? t('addAccount') : u.name, isNew ? t('addAccountHint') : null, fields, t('save'), function() {
		var lim = Math.round((parseFloat(limit.value) || 0) * 1e9);
		if (isNew)
			return api('POST', '/api/sharing/users', { name: name.value, limit: lim }).then(function(r) {
				S.sharing = r; renderPanel();
				var made = r.users.filter(function(x) { return x.name === name.value.trim(); })[0];
				if (made) setTimeout(function() { accountLink(made); }, 50);
			});
		return api('PUT', '/api/sharing/users/' + u.id, { name: name.value, limit: lim, reset_usage: reset.checked, new_link: newLink.checked })
			.then(function(r) { S.sharing = r; renderPanel(); if (newLink.checked) toast(t('newLinkDone')); });
	});
}

// The account's link and QR, plus a ready message with how to use it.
function accountLink(u) {
	api('GET', '/api/sharing/users/' + u.id + '/link').then(function(r) {
		linkDialog(r.name, r, t('howToUse'));
	}).catch(function(e) { toast(e.message); });
}

function warpLine() {
	var w = S.warp;
	if (!S.settings || !S.settings.warp_backup) return [];
	if (!w || !w.registered) return [h('small', {}, t('warpWait'))];
	var total = Object.keys(w.endpoints || {}).length;
	return [h('small', { 'class': 'num' }, [
		w.active ? h('b', { style: 'color:var(--fox)' }, t('warpActive') + '. ') : null,
		total ? (w.working ? t('warpReady', w.working, total) : t('warpNone')) : t('warpWait')
	])];
}

/* ---------- log */

function panelLogs() {
	var pre = h('pre', { 'class': 'fgz-log', id: 'fgz-log' }, S.logLines.map(logLine));
	setTimeout(function() { pre.scrollTop = pre.scrollHeight; }, 0);
	return h('div', { 'class': 'fgz-panel' }, [pre]);
}

function logLine(l) {
	var d = new Date(l.time * 1000);
	var p = function(x) { return (x < 10 ? '0' : '') + x; };
	var bad = /error|fatal|failed|rejected/i.test(l.text);
	return h('div', { 'class': bad ? 'err' : null }, [
		p(d.getHours()) + ':' + p(d.getMinutes()) + ':' + p(d.getSeconds()) + ' ',
		h('span', { 'class': 'src' }, l.source), ' ' + l.text
	]);
}

function pollLogs() {
	return api('GET', '/api/logs?since=' + S.logSeq).then(function(lines) {
		if (!lines.length) return;
		S.logSeq = lines[lines.length - 1].seq;
		S.logLines = S.logLines.concat(lines).slice(-600);
		var pre = document.getElementById('fgz-log');
		if (pre) {
			var atEnd = pre.scrollHeight - pre.scrollTop - pre.clientHeight < 40;
			lines.forEach(function(l) { pre.appendChild(logLine(l)); });
			if (atEnd) pre.scrollTop = pre.scrollHeight;
		}
	});
}

/* ------------------------------------------------------------ live data */

function refreshStatus() {
	return api('GET', '/api/status').then(function(s) {
		var changed = !S.status || s.phase !== S.status.phase || s.current !== S.status.current || S.offline;
		S.status = s;
		S.offline = false;
		renderHero();
		if (changed && S.tab === 'nodes') renderPanel();
	}).catch(function() {
		if (!S.offline) { S.offline = true; renderHero(); }
	});
}

var es;
function streamTraffic() {
	if (es || typeof EventSource === 'undefined') return;
	es = new EventSource(BASE + '/api/traffic?sid=' + encodeURIComponent(SID));
	es.onmessage = function(e) {
		try { S.speed = JSON.parse(e.data); } catch (x) { return; }
		S.hist.push({ t: Date.now(), up: S.speed.up, down: S.speed.down });
		if (S.hist.length > HIST_MAX) S.hist.shift();
		if (S.tab === 'network') drawTraffic();
		var el = document.getElementById('fgz-speed');
		if (el) el.replaceChildren.apply(el, speedKids());
		var live = document.getElementById('fgz-live');
		if (live) live.replaceChildren.apply(live, liveKids());
		var up = document.getElementById('fgz-uptime');
		if (up && S.status) up.textContent = duration(Date.now() / 1000 - S.status.since);
	};
	es.onerror = function() { es.close(); es = null; setTimeout(streamTraffic, 1000); };
}

function tick() {
	if (document.visibilityState !== 'visible') return;
	refreshStatus();
	if (S.tab === 'logs') pollLogs();
	if (S.tab === 'settings' && S.settings && S.settings.warp_backup)
		api('GET', '/api/warp').then(function(r) { S.warp = r; var el = document.getElementById('fgz-warp'); if (el) el.replaceChildren.apply(el, warpLine()); }).catch(function() {});
	if (S.tab === 'network')
		api('GET', '/api/devices').then(function(r) { S.devices = r; drawMap(); }).catch(function() {});
	// Anti-lag installing or starting: follow it until it is on.
	if (S.tab === 'games' && S.lag && (S.lag.installing || (S.lag.on && !S.lag.active && !S.lag.error)) && !document.querySelector('.fgz input:focus'))
		api('GET', '/api/antilag').then(function(r) { S.lag = r; renderPanel(); }).catch(function() {});
	if (S.tab === 'share' && !document.querySelector('.fgz-dialog[open], .fgz input:focus'))
		api('GET', '/api/sharing').then(function(r) { S.sharing = r; renderPanel(); }).catch(function() {});
	if (S.tab === 'devices' && !document.querySelector('.fgz-dev select:focus, .fgz-dialog[open]'))
		api('GET', '/api/devices').then(function(r) { S.devices = r; renderPanel(); }).catch(function() {});
}

return view.extend({
	load: function() {
		document.head.appendChild(h('link', { rel: 'stylesheet', href: RES + '/app.css?v=' + VERSION }));
		return Promise.all([
			api('GET', '/api/settings').catch(function() { return null; }),
			api('GET', '/api/status').catch(function() { return null; }),
			api('GET', '/api/subs').catch(function() { return []; }),
			api('GET', '/api/nodes').catch(function() { return []; }),
			api('GET', '/api/usage').catch(function() { return null; })
		]);
	},

	render: function(data) {
		S.settings = data[0];
		S.status = data[1];
		S.subs = data[2] || [];
		S.nodes = data[3] || [];
		S.usage = data[4];
		S.offline = !data[1];
		lang = (S.settings && S.settings.language) || (/^fa/.test(L.env.lang || navigator.language) ? 'fa' : 'en');
		root = h('div', { 'class': 'fgz' });
		applyTheme();
		render();
		loadTab();
		streamTraffic();
		setInterval(tick, 2000);
		document.addEventListener('visibilitychange', tick);
		window.addEventListener('resize', function() { if (S.tab === 'network') { drawTraffic(); drawMap(); } });
		return root;
	},

	handleSave: null,
	handleSaveApply: null,
	handleReset: null
});
