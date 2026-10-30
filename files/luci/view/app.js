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

/* ------------------------------------------------------------ text */

var T = {
	en: {
		connected: 'Connected', disconnected: 'Not connected', starting: 'Connecting…',
		installing: 'Getting ready…', failed: 'Could not connect',
		via: 'Through {0}', idle: 'Your network uses its normal connection.',
		connect: 'Connect', disconnect: 'Disconnect',
		yourNet: 'Your network', exit: 'Exit',
		up: 'Up', down: 'Down', delay: 'Delay', used: 'Used', conns: 'Connections', upFor: 'Connected for',
		nodes: 'Servers', subs: 'Subscriptions', rules: 'Routing', settings: 'Settings', logs: 'Log',
		auto: 'Fastest, automatically', autoHint: 'Switches by itself when a server slows down or fails',
		addedByYou: 'Added by you', add: 'Add servers', testAll: 'Test all', removeFailed: 'Remove failed',
		search: 'Search servers', test: 'Test', remove: 'Remove', failedShort: 'failed',
		noNodes: 'No servers yet', noNodesHint: 'Add share links (vless://, vmess://, trojan://, ss://, hy2:// …) or a subscription.',
		addTitle: 'Add servers', addHint: 'Paste one or more share links, or a sing-box JSON config.',
		cancel: 'Cancel', added: 'Added {0} servers', skipped: '{0} lines could not be read',
		testing: 'Testing…', tested: '{0} of {1} servers answered', removed: 'Removed {0} servers',
		connectFirst: 'Connect first: servers are tested through the tunnel.',
		addSub: 'Add subscription', subUrl: 'Subscription link', subName: 'Name (optional)',
		update: 'Update', updated: 'Updated', noSubs: 'No subscriptions', noSubsHint: 'A subscription keeps its servers up to date by itself.',
		servers: '{0} servers', expires: 'Expires in {0} days', expired: 'Expired', lastUpdate: 'Updated {0}',
		every: 'Every {0} hours', of: '{0} of {1}',
		iranDirect: 'Iranian sites go direct', iranDirectHint: 'Faster, and does not use your server’s traffic',
		blockAds: 'Block ads', blockAdsHint: 'Blocks known ad and tracker domains for every device',
		quicBlock: 'Block QUIC', quicBlockHint: 'Makes video and browsers use TCP; helps on some servers',
		customRules: 'Your rules', addRule: 'Add rule', rulePlaceholder: 'Domains or IP ranges, separated by commas',
		toDirect: 'Direct', toProxy: 'Through the tunnel', toBlock: 'Block', save: 'Save', saved: 'Saved',
		noRules: 'No rules yet. Example: openai.com through a specific server.',
		sConnection: 'Connection', sDns: 'DNS', sNetwork: 'Network', sAdvanced: 'Advanced', sCores: 'Cores',
		testUrl: 'Test address', testUrlHint: 'Used to measure every server',
		testInterval: 'Test every (seconds)', tolerance: 'Switch when faster by (ms)', toleranceHint: 'Avoids hopping between servers that are nearly as fast',
		remoteDns: 'DNS through the tunnel', directDns: 'DNS for direct sites', fakeIp: 'Fake IP', fakeIpHint: 'Faster first connections; some apps dislike it',
		proxyRouter: 'Router’s own traffic through the tunnel', ipv6: 'IPv6', killSwitch: 'Kill switch', killSwitchHint: 'Devices stay offline while the tunnel is down, instead of going out unprotected',
		lan: 'LAN interfaces', lanHint: 'Comma-separated, usually br-lan',
		mux: 'Multiplex', muxHint: 'Fewer handshakes on servers that support it',
		fragment: 'TLS fragment', fragmentHint: 'Splits the TLS hello; can get past some filtering',
		keepWarm: 'Keep core ready', keepWarmHint: 'Connect takes under a second; uses about 40 MB of memory',
		logLevel: 'Log detail', mirror: 'GitHub mirror', mirrorHint: 'Used when GitHub is unreachable',
		coreBeta: 'Use beta cores',
		installed: 'Installed: {0}', notInstalled: 'Not installed', updateCore: 'Update {0}', updateRules: 'Update rule-sets',
		updating: 'Updating…', theme: 'Theme', lang: 'فارسی',
		offline: 'The FoxGozar service is not answering. It may be starting; this page retries by itself.',
		justNow: 'just now', minAgo: '{0} min ago', hAgo: '{0} h ago', dAgo: '{0} d ago'
	},
	fa: {
		connected: 'وصل است', disconnected: 'وصل نیست', starting: 'در حال اتصال…',
		installing: 'در حال آماده‌سازی…', failed: 'اتصال برقرار نشد',
		via: 'از طریق {0}', idle: 'شبکه شما از اینترنت عادی استفاده می‌کند.',
		connect: 'اتصال', disconnect: 'قطع اتصال',
		yourNet: 'شبکه شما', exit: 'خروجی',
		up: 'ارسال', down: 'دریافت', delay: 'پینگ', used: 'مصرف', conns: 'اتصال‌ها', upFor: 'مدت اتصال',
		nodes: 'سرورها', subs: 'اشتراک‌ها', rules: 'مسیریابی', settings: 'تنظیمات', logs: 'گزارش',
		auto: 'سریع‌ترین، خودکار', autoHint: 'وقتی سروری کند یا قطع شود، خودش عوض می‌کند',
		addedByYou: 'افزوده‌شده توسط شما', add: 'افزودن سرور', testAll: 'تست همه', removeFailed: 'حذف خراب‌ها',
		search: 'جستجوی سرور', test: 'تست', remove: 'حذف', failedShort: 'خراب',
		noNodes: 'هنوز سروری ندارید', noNodesHint: 'لینک کانفیگ (vless://، vmess://، trojan://، ss://، hy2:// …) یا یک اشتراک اضافه کنید.',
		addTitle: 'افزودن سرور', addHint: 'یک یا چند لینک کانفیگ، یا کانفیگ JSON سینگ‌باکس را بچسبانید.',
		cancel: 'انصراف', added: '{0} سرور اضافه شد', skipped: '{0} خط خوانده نشد',
		testing: 'در حال تست…', tested: '{0} از {1} سرور جواب دادند', removed: '{0} سرور حذف شد',
		connectFirst: 'اول وصل شوید: تست سرورها از داخل تونل انجام می‌شود.',
		addSub: 'افزودن اشتراک', subUrl: 'لینک اشتراک', subName: 'نام (اختیاری)',
		update: 'به‌روزرسانی', updated: 'به‌روز شد', noSubs: 'اشتراکی ندارید', noSubsHint: 'اشتراک، سرورهایش را خودش به‌روز نگه می‌دارد.',
		servers: '{0} سرور', expires: '{0} روز مانده', expired: 'منقضی شده', lastUpdate: 'به‌روزرسانی {0}',
		every: 'هر {0} ساعت', of: '{0} از {1}',
		iranDirect: 'سایت‌های ایرانی مستقیم', iranDirectHint: 'سریع‌تر است و از حجم سرور شما کم نمی‌کند',
		blockAds: 'مسدودسازی تبلیغات', blockAdsHint: 'دامنه‌های تبلیغاتی و ردیاب برای همه دستگاه‌ها بسته می‌شوند',
		quicBlock: 'بستن QUIC', quicBlockHint: 'ویدیو و مرورگر از TCP استفاده می‌کنند؛ روی بعضی سرورها بهتر است',
		customRules: 'قوانین شما', addRule: 'افزودن قانون', rulePlaceholder: 'دامنه یا رنج IP، با کاما جدا کنید',
		toDirect: 'مستقیم', toProxy: 'از تونل', toBlock: 'مسدود', save: 'ذخیره', saved: 'ذخیره شد',
		noRules: 'هنوز قانونی ندارید. مثلاً openai.com از یک سرور مشخص.',
		sConnection: 'اتصال', sDns: 'DNS', sNetwork: 'شبکه', sAdvanced: 'پیشرفته', sCores: 'هسته‌ها',
		testUrl: 'آدرس تست', testUrlHint: 'برای اندازه‌گیری همه سرورها',
		testInterval: 'تست هر چند ثانیه', tolerance: 'عوض کن وقتی این‌قدر سریع‌تر است (ms)', toleranceHint: 'از پریدن بین سرورهای تقریباً هم‌سرعت جلوگیری می‌کند',
		remoteDns: 'DNS از داخل تونل', directDns: 'DNS سایت‌های مستقیم', fakeIp: 'Fake IP', fakeIpHint: 'اتصال اول سریع‌تر؛ بعضی برنامه‌ها با آن مشکل دارند',
		proxyRouter: 'ترافیک خود روتر از تونل', ipv6: 'IPv6', killSwitch: 'کیل‌سوییچ', killSwitchHint: 'وقتی تونل قطع است دستگاه‌ها آفلاین می‌مانند، نه بدون محافظت',
		lan: 'اینترفیس‌های LAN', lanHint: 'با کاما جدا کنید، معمولاً br-lan',
		mux: 'Multiplex', muxHint: 'روی سرورهایی که پشتیبانی می‌کنند، هندشیک کمتر',
		fragment: 'TLS fragment', fragmentHint: 'Hello تی‌ال‌اس را تکه می‌کند؛ گاهی از فیلتر رد می‌شود',
		keepWarm: 'هسته آماده بماند', keepWarmHint: 'اتصال زیر یک ثانیه؛ حدود ۴۰ مگابایت رم',
		logLevel: 'جزئیات گزارش', mirror: 'میرور GitHub', mirrorHint: 'وقتی GitHub در دسترس نیست',
		coreBeta: 'هسته‌های بتا',
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
	power: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round"><path d="M12 3v8"/><path d="M6.3 6.8a8 8 0 1 0 11.4 0"/></svg>',
	bolt: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linejoin="round"><path d="M13 3 5 14h6l-1 7 8-11h-6z"/></svg>',
	trash: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M4 7h16M10 11v6M14 11v6M6 7l1 13h10l1-13M9 7V4h6v3"/></svg>',
	refresh: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M20 11a8 8 0 1 0-2.3 5.7M20 4v7h-7"/></svg>',
	fox: '<svg viewBox="0 0 48 48"><path d="M6 6l12 9h12l12-9-3 18-15 18L9 24z" fill="#F59E0B"/><path d="M18 15h12l-6 8z" fill="#fff" opacity=".9"/><path d="M15 26l9 16 9-16-9 5z" fill="#fff"/><circle cx="17.5" cy="22" r="2" fill="#0E1730"/><circle cx="30.5" cy="22" r="2" fill="#0E1730"/><path d="M22 37h4l-2 3z" fill="#0E1730"/></svg>'
};
function icon(name) { var s = h('span', { 'aria-hidden': 'true', html: ICON[name] }); return s.firstChild; }

/* ------------------------------------------------------------ the app */

var S = {
	tab: 'nodes', status: null, nodes: [], subs: [], rules: [], settings: null, cores: {},
	speed: { up: 0, down: 0 }, filter: '', offline: false, logSeq: 0, logLines: []
};
var root, heroEl, panelEl, tabsEl;

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
	var sub = phase === 'connected' && st.current_name ? t('via', st.current_name)
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

	var facts = [];
	if (phase === 'connected') {
		if (st.delay) facts.push(h('span', {}, [t('delay') + ' ', h('b', { 'class': 'num', dir: 'ltr' }, num(st.delay) + ' ms')]));
		facts.push(h('span', {}, [t('used') + ' ', h('b', { 'class': 'num', dir: 'ltr' }, bytes((st.upload || 0) + (st.download || 0)))]));
		facts.push(h('span', {}, [t('conns') + ' ', h('b', { 'class': 'num' }, num(st.connections || 0))]));
		facts.push(h('span', {}, [t('upFor') + ' ', h('b', { 'class': 'num', id: 'fgz-uptime' }, duration(Date.now() / 1000 - st.since))]));
	}

	heroEl.replaceChildren.apply(heroEl, present([
		h('div', { 'class': 'fgz-hero-top' }, [
			h('div', { 'class': 'fgz-state' }, [h('h2', {}, title), sub ? h('p', {}, sub) : null]),
			power
		]),
		h('div', { 'class': 'fgz-passage', 'aria-hidden': 'true' }, [
			h('div', { 'class': 'fgz-end' }, [h('b', {}, t('yourNet'))]),
			h('div', { 'class': 'fgz-mid' }, [
				phase === 'connected' ? h('div', { 'class': 'fgz-speed num', id: 'fgz-speed', dir: 'ltr' }, speedKids()) : null,
				h('div', { 'class': 'fgz-track' }, [h('i'), h('i')])
			]),
			h('div', { 'class': 'fgz-end' }, [h('b', {}, phase === 'connected' ? (countryOf(st.current_name) || t('exit')) : t('exit'))])
		]),
		facts.length ? h('div', { 'class': 'fgz-facts' }, facts) : null,
		phase === 'failed' && st.message ? h('div', { 'class': 'fgz-alert', role: 'alert' }, st.message) : null,
		S.offline ? h('div', { 'class': 'fgz-alert', role: 'alert' }, t('offline')) : null
	]));
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

/* ---------- tabs */

var TABS = ['nodes', 'subs', 'rules', 'settings', 'logs'];
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
	case 'nodes': p = Promise.all([api('GET', '/api/nodes'), api('GET', '/api/subs')]).then(function(r) { S.nodes = r[0]; S.subs = r[1]; }); break;
	case 'subs': p = api('GET', '/api/subs').then(function(r) { S.subs = r; }); break;
	case 'rules': p = Promise.all([api('GET', '/api/rules'), api('GET', '/api/nodes')]).then(function(r) { S.rules = r[0]; S.nodes = r[1]; }); break;
	case 'settings': p = api('GET', '/api/core').then(function(r) { S.cores = r; }); break;
	case 'logs': S.logSeq = 0; S.logLines = []; p = pollLogs(); break;
	}
	renderPanel();
	return (p || Promise.resolve()).then(renderPanel).catch(function(e) { toast(e.message); });
}

function renderPanel() {
	var fn = { nodes: panelNodes, subs: panelSubs, rules: panelRules, settings: panelSettings, logs: panelLogs }[S.tab];
	panelEl.replaceChildren(fn());
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
		h('div', { 'class': 'name' }, [h('b', { 'class': live ? 'live' : null, dir: 'auto' }, name), sub ? h('small', { dir: 'ltr' }, sub) : null]),
		id === 'auto' || !delayText ? h('span') : h('span', { 'class': 'fgz-delay num', 'data-q': q, dir: delay > 0 ? 'ltr' : null }, delayText),
		id === 'auto' ? h('span') : h('span', { style: 'display:flex' }, [
			h('button', { 'class': 'fgz-icon', title: t('test'), 'aria-label': t('test'), onclick: function(e) {
				e.stopPropagation();
				if (!S.status || S.status.phase !== 'connected') { toast(t('connectFirst')); return; }
				busy(e.currentTarget, api('POST', '/api/test/' + id).then(function(r) {
					S.nodes.forEach(function(n) { if (n.id === id) n.delay = r.delay; });
					renderPanel();
				}));
			} }, [icon('bolt')]),
			h('button', { 'class': 'fgz-icon', title: t('remove'), 'aria-label': t('remove'), onclick: function(e) {
				e.stopPropagation();
				api('DELETE', '/api/nodes/' + id).then(loadTab).catch(function(err) { toast(err.message); });
			} }, [icon('trash')])
		])
	]);
	return el;
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
		h('div', { 'class': 'actions' }, [
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

function panelRules() {
	var s = S.settings || {};
	var toggles = h('div', { 'class': 'fgz-panel fgz-section' }, [
		switchField('iran_direct', t('iranDirect'), t('iranDirectHint')),
		switchField('block_ads', t('blockAds'), t('blockAdsHint')),
		switchField('quic_block', t('quicBlock'), t('quicBlockHint'))
	]);

	var targets = [['direct', t('toDirect')], ['proxy', t('toProxy')], ['block', t('toBlock')]]
		.concat(S.nodes.map(function(n) { return [n.id, n.name]; }));

	var list = S.rules.map(function(r, i) {
		var text = (r.domains || []).concat(r.ips || []).join(', ');
		return h('div', { 'class': 'fgz-rule' }, [
			h('input', { type: 'checkbox', 'class': 'fgz-switch', checked: r.enabled, 'aria-label': t('customRules'),
				onchange: function(e) { r.enabled = e.target.checked; } }),
			h('input', { type: 'text', 'class': 'ltr', value: text, placeholder: t('rulePlaceholder'),
				onchange: function(e) {
					var parts = e.target.value.split(/[,\s]+/).filter(Boolean);
					r.ips = parts.filter(function(p) { return /^[\d.:a-f]+\/\d+$|^\d+\.\d+\.\d+\.\d+$/i.test(p); });
					r.domains = parts.filter(function(p) { return r.ips.indexOf(p) < 0; });
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
		])
	].concat(list.length ? list : [h('div', { 'class': 'fgz-empty' }, t('noRules'))]));

	return h('div', {}, [toggles, custom]);
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
			inputField('test_url', t('testUrl'), t('testUrlHint'), 'url'),
			inputField('test_interval', t('testInterval'), null, 'number', int),
			inputField('tolerance', t('tolerance'), t('toleranceHint'), 'number', int),
			switchField('keep_warm', t('keepWarm'), t('keepWarmHint'))
		]),
		section(t('sDns'), [
			inputField('remote_dns', t('remoteDns'), 'https://1.1.1.1/dns-query'),
			inputField('direct_dns', t('directDns'), 'udp://8.8.8.8'),
			switchField('fake_ip', t('fakeIp'), t('fakeIpHint'))
		]),
		section(t('sNetwork'), [
			switchField('proxy_router', t('proxyRouter')),
			switchField('kill_switch', t('killSwitch'), t('killSwitchHint')),
			switchField('ipv6', t('ipv6')),
			inputField('lan_interfaces', t('lan'), t('lanHint'), 'text', list, joined)
		]),
		section(t('sAdvanced'), [
			switchField('mux', t('mux'), t('muxHint')),
			switchField('tls_fragment', t('fragment'), t('fragmentHint')),
			selectField('log_level', t('logLevel'), ['error', 'warn', 'info', 'debug']),
			inputField('github_mirror', t('mirror'), t('mirrorHint'), 'url')
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
		var el = document.getElementById('fgz-speed');
		if (el) el.replaceChildren.apply(el, speedKids());
		var up = document.getElementById('fgz-uptime');
		if (up && S.status) up.textContent = duration(Date.now() / 1000 - S.status.since);
	};
	es.onerror = function() { es.close(); es = null; setTimeout(streamTraffic, 1000); };
}

function tick() {
	if (document.visibilityState !== 'visible') return;
	refreshStatus();
	if (S.tab === 'logs') pollLogs();
}

return view.extend({
	load: function() {
		document.head.appendChild(h('link', { rel: 'stylesheet', href: RES + '/app.css' }));
		return Promise.all([
			api('GET', '/api/settings').catch(function() { return null; }),
			api('GET', '/api/status').catch(function() { return null; })
		]);
	},

	render: function(data) {
		S.settings = data[0];
		S.status = data[1];
		S.offline = !data[1];
		lang = (S.settings && S.settings.language) || (/^fa/.test(L.env.lang || navigator.language) ? 'fa' : 'en');
		root = h('div', { 'class': 'fgz' });
		applyTheme();
		render();
		loadTab();
		streamTraffic();
		setInterval(tick, 2000);
		document.addEventListener('visibilitychange', tick);
		return root;
	},

	handleSave: null,
	handleSaveApply: null,
	handleReset: null
});
