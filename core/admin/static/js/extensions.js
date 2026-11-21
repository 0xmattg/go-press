// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

// Theme market, theme package management and developer options.
//
// The pages that load this script embed their texts as JSON in
// #extensionsText. Everything the market sends (titles, summaries, hosts,
// URLs) is inserted as text, never as HTML: the catalog is signed, but a
// compromised market must not become a way into the admin.
(function () {
    'use strict';

    var TEXT = {};
    try {
        var textNode = document.getElementById('extensionsText');
        TEXT = JSON.parse(textNode ? textNode.textContent : '{}') || {};
    } catch (e) {
        TEXT = {};
    }

    var PAGE_SIZE = 48;

    // t returns a text, replacing %s and %d with the arguments in order.
    function t(key) {
        var args = Array.prototype.slice.call(arguments, 1);
        var s = Object.prototype.hasOwnProperty.call(TEXT, key) ? TEXT[key] : key;
        var i = 0;
        return String(s).replace(/%[sdv]/g, function () {
            return i < args.length ? String(args[i++]) : '';
        });
    }

    function has(key) {
        return Object.prototype.hasOwnProperty.call(TEXT, key) && TEXT[key] !== '';
    }

    // el builds an element. attrs.text sets textContent; false or null
    // attributes are left out; children are nodes or strings.
    function el(tag, attrs, children) {
        var node = document.createElement(tag);
        if (attrs) {
            Object.keys(attrs).forEach(function (key) {
                var value = attrs[key];
                if (value === false || value === null || value === undefined) return;
                if (key === 'text') node.textContent = value;
                else node.setAttribute(key, value === true ? '' : value);
            });
        }
        append(node, children);
        return node;
    }

    function append(node, children) {
        if (children === null || children === undefined) return node;
        if (!Array.isArray(children)) children = [children];
        children.forEach(function (child) {
            if (child === null || child === undefined || child === false) return;
            node.appendChild(typeof child === 'string' ? document.createTextNode(child) : child);
        });
        return node;
    }

    function clear(node) {
        while (node.firstChild) node.removeChild(node.firstChild);
    }

    function formatSize(bytes) {
        if (!bytes && bytes !== 0) return '';
        if (bytes < 1024) return bytes + ' B';
        if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB';
        return (bytes / (1024 * 1024)).toFixed(1) + ' MB';
    }

    function formatDate(value) {
        var d = new Date(value);
        if (isNaN(d.getTime())) return '';
        try {
            return d.toLocaleString(document.documentElement.lang || undefined);
        } catch (e) {
            return d.toLocaleString();
        }
    }

    function safeHTTPS(url) {
        return typeof url === 'string' && /^https:\/\//i.test(url) ? url : '';
    }

    // request sends a same-origin request and resolves to the JSON body
    // with its HTTP status. An expired session redirects to the login page,
    // which is reported as such.
    function request(method, url, body) {
        var options = { method: method, credentials: 'same-origin', headers: { 'Accept': 'application/json' } };
        if (body) options.body = body;
        return fetch(url, options).then(function (res) {
            if (res.redirected && /\/admin\/login/.test(res.url)) {
                return { ok: false, status: res.status, error: { code: 'session_expired' } };
            }
            return res.json().catch(function () { return null; }).then(function (data) {
                if (!data || typeof data !== 'object') {
                    data = { ok: false, error: { code: res.status === 403 ? 'forbidden' : 'internal' } };
                }
                data.status = res.status;
                return data;
            });
        }, function () {
            return { ok: false, status: 0, error: { code: 'network' } };
        });
    }

    function errorText(err) {
        if (!err) return t('error.generic');
        if ((err.code === 'catalog_unavailable' || err.code === 'market_disabled') && err.reason && has('catalog_reason.' + err.reason)) {
            return t('catalog_reason.' + err.reason);
        }
        return has('error.' + err.code) ? t('error.' + err.code) : t('error.generic');
    }

    // ---------------------------------------------------------------
    // Dialogs: native <dialog> with showModal, so focus stays inside, the
    // page behind cannot be used, and Esc closes. Focus returns to the
    // control that opened it.
    // ---------------------------------------------------------------

    var dialogCount = 0;

    // openDialog options:
    //   title, body (nodes), opener (element to refocus), danger (bool),
    //   ack (checkbox label; submitting needs it checked),
    //   password (label; submitting needs a value),
    //   submitLabel, busyLabel, cancelLabel,
    //   onSubmit(ctx): a value or promise. Resolving to {error: text} keeps
    //   the dialog open with the error; false means ctx.close() was called;
    //   anything else closes it.
    function openDialog(opts) {
        var id = 'extDialog' + (++dialogCount);
        var busy = false;
        var dialog = el('dialog', { 'class': 'ext-dialog', 'aria-labelledby': id + 'Title', 'aria-describedby': id + 'Body' });
        var form = el('form', { 'class': 'ext-dialog-form', method: 'dialog', novalidate: true });
        var closeButton = el('button', { type: 'button', 'class': 'modal-close', 'aria-label': t('close'), title: t('close') }, '×');
        var header = el('div', { 'class': 'modal-header' }, [el('h3', { id: id + 'Title', text: opts.title }), closeButton]);
        var body = el('div', { 'class': 'modal-body', id: id + 'Body' }, opts.body);

        var ack = null;
        if (opts.ack) {
            ack = el('input', { type: 'checkbox', id: id + 'Ack' });
            body.appendChild(el('label', { 'class': 'checkbox-label ext-ack', 'for': id + 'Ack' }, [ack, el('span', { text: opts.ack })]));
        }
        var password = null;
        if (opts.password) {
            password = el('input', { type: 'password', id: id + 'Password', autocomplete: 'current-password', required: true });
            body.appendChild(el('div', { 'class': 'form-group ext-password' }, [el('label', { 'for': id + 'Password', text: opts.password }), password]));
        }
        var error = el('div', { 'class': 'ext-dialog-error', role: 'alert', hidden: true });
        body.appendChild(error);

        var cancel = el('button', { type: 'button', 'class': 'btn btn-secondary', text: opts.cancelLabel || t('cancel') });
        var submit = el('button', { type: 'submit', 'class': 'btn ' + (opts.danger ? 'btn-danger' : 'btn-primary'), text: opts.submitLabel });
        var footer = el('div', { 'class': 'modal-footer' }, [cancel, submit]);
        append(form, [header, body, footer]);
        dialog.appendChild(form);
        if (opts.danger) dialog.classList.add('ext-dialog-danger');

        function ready() {
            return (!ack || ack.checked) && (!password || password.value !== '');
        }
        function sync() {
            submit.disabled = busy || !ready();
        }
        function setBusy(value) {
            busy = value;
            cancel.disabled = value;
            closeButton.disabled = value;
            submit.textContent = value ? (opts.busyLabel || t('working')) : opts.submitLabel;
            sync();
        }
        function showError(content) {
            clear(error);
            append(error, content);
            error.hidden = false;
        }
        function close() {
            if (dialog.open) dialog.close();
        }
        var ctx = {
            dialog: dialog,
            close: close,
            showError: showError,
            password: function () { return password ? password.value : ''; },
            clearPassword: function () { if (password) password.value = ''; sync(); }
        };

        if (ack) ack.addEventListener('change', sync);
        if (password) password.addEventListener('input', sync);
        cancel.addEventListener('click', close);
        closeButton.addEventListener('click', close);
        // Esc must not close a dialog whose request is running.
        dialog.addEventListener('cancel', function (event) {
            if (busy) event.preventDefault();
        });
        dialog.addEventListener('close', function () {
            if (dialog.parentNode) dialog.parentNode.removeChild(dialog);
            if (opts.opener && document.body.contains(opts.opener)) {
                opts.opener.focus();
            } else if (noticeBox && noticeBox.firstChild) {
                // The opener was re-rendered (an installed card): move focus
                // to the notice that reports the result.
                noticeBox.setAttribute('tabindex', '-1');
                noticeBox.focus();
            }
        });
        form.addEventListener('submit', function (event) {
            event.preventDefault();
            if (busy || !ready()) return;
            error.hidden = true;
            setBusy(true);
            Promise.resolve(opts.onSubmit ? opts.onSubmit(ctx) : true).then(function (result) {
                if (result === false) return;
                if (result && result.error) {
                    setBusy(false);
                    showError(result.error);
                    return;
                }
                close();
            }, function () {
                setBusy(false);
                showError(t('error.generic'));
            });
        });

        document.body.appendChild(dialog);
        sync();
        if (typeof dialog.showModal === 'function') {
            dialog.showModal();
        } else {
            dialog.setAttribute('open', '');
        }
        var first = ack || password || (opts.danger ? cancel : submit);
        first.focus();
        return ctx;
    }

    function details(rows) {
        var list = el('dl', { 'class': 'ext-details' });
        rows.forEach(function (row) {
            if (!row) return;
            list.appendChild(el('dt', { text: row[0] }));
            list.appendChild(el('dd', row[2] ? { 'class': row[2] } : null, row[1]));
        });
        return list;
    }

    function hostList(hosts) {
        if (!hosts || !hosts.length) return el('p', { 'class': 'ext-muted', text: t('external_none') });
        return el('ul', { 'class': 'ext-hosts' }, hosts.map(function (host) { return el('li', { text: host }); }));
    }

    function requiresText(requires) {
        if (!requires || !requires.length) return t('requires_none');
        return requires.map(function (r) {
            var s = r.slug + (r.version ? ' ' + r.version : '');
            return r.optional ? s + ' (' + t('optional') + ')' : s;
        }).join(', ');
    }

    // ---------------------------------------------------------------
    // Notices above the market panel. They stay until the next action.
    // ---------------------------------------------------------------

    var noticeBox = document.querySelector('[data-ext-notice]');

    function notice(kind, content) {
        if (!noticeBox) return;
        clear(noticeBox);
        noticeBox.appendChild(el('div', { 'class': 'alert ' + (kind === 'error' ? 'alert-error' : 'alert-success') }, content));
    }

    // ---------------------------------------------------------------
    // Market page
    // ---------------------------------------------------------------

    var market = document.querySelector('[data-ext-market]');
    var can = {
        install: !!market && market.getAttribute('data-can-install') === 'true',
        remove: !!market && market.getAttribute('data-can-delete') === 'true',
        developer: !!market && market.getAttribute('data-can-developer') === 'true',
        switchTheme: !!market && market.getAttribute('data-can-switch') === 'true'
    };

    function themesLink() {
        if (!can.switchTheme) return null;
        return el('a', { href: '/admin/themes', text: t('activate_link') });
    }

    function installedNotice(name, version) {
        notice('success', [t('installed_notice', name, version), ' ', themesLink()]);
    }

    function badge(className, text, title) {
        return el('span', { 'class': 'badge ' + className, text: text, title: title || null });
    }

    function initBrowse() {
        var state = market.querySelector('[data-ext-state]');
        var list = market.querySelector('[data-ext-list]');
        var toolbar = market.querySelector('[data-ext-toolbar]');
        var search = market.querySelector('[data-ext-search]');
        var moreWrap = market.querySelector('[data-ext-more-wrap]');
        var more = market.querySelector('[data-ext-more]');
        var revokedBox = market.querySelector('[data-ext-revoked]');
        var items = [];
        var shown = PAGE_SIZE;

        function setState(content, className) {
            clear(state);
            state.className = 'market-state' + (className ? ' ' + className : '');
            append(state, content);
            state.hidden = false;
        }

        function load() {
            setState([el('span', { 'class': 'market-spinner', 'aria-hidden': 'true' }), t('loading_catalog')]);
            clear(list);
            toolbar.hidden = true;
            moreWrap.hidden = true;
            revokedBox.hidden = true;
            request('GET', '/admin/extensions/themes/catalog').then(function (data) {
                if (!data.ok) {
                    var retry = el('button', { type: 'button', 'class': 'btn btn-secondary btn-sm', text: t('retry') });
                    retry.addEventListener('click', load);
                    setState([el('strong', { text: t('catalog_failed') }), el('p', { text: errorText(data.error) }), retry], 'market-state-error');
                    return;
                }
                items = data.catalog.items || [];
                showRevoked(data.catalog.revoked_installed || []);
                shown = PAGE_SIZE;
                revealHash();
            });
        }

        // revealHash renders the list and, when the address names a theme
        // (#market-<slug>, as the update badges link), shows that card even
        // beyond the first page, scrolls to it and gives it focus.
        function revealHash() {
            var slug = location.hash.indexOf('#market-') === 0 ? decodeURIComponent(location.hash.slice(8)) : '';
            var index = -1;
            items.forEach(function (item, i) { if (item.slug === slug) index = i; });
            if (index >= shown) shown = (Math.floor(index / PAGE_SIZE) + 1) * PAGE_SIZE;
            render();
            var target = index >= 0 ? document.getElementById('market-' + slug) : null;
            if (!target) return;
            target.setAttribute('tabindex', '-1');
            target.classList.add('market-card-target');
            target.scrollIntoView({ block: 'center' });
            target.focus({ preventScroll: true });
        }

        function showRevoked(revoked) {
            clear(revokedBox);
            if (!revoked.length) return;
            revokedBox.appendChild(el('strong', { text: t('revoked_installed_title') }));
            revokedBox.appendChild(el('ul', null, revoked.map(function (r) {
                var reason = has('revoked_reason.' + r.reason) ? t('revoked_reason.' + r.reason) : t('revoked_reason.other');
                return el('li', null, [el('code', { text: r.slug + ' ' + r.version }), ' — ' + reason + (r.note ? ' (' + r.note + ')' : '')]);
            })));
            revokedBox.hidden = false;
        }

        function matches(item, q) {
            return [item.title, item.summary, item.author, item.slug].some(function (v) {
                return v && String(v).toLowerCase().indexOf(q) !== -1;
            });
        }

        function render() {
            var q = search.value.trim().toLowerCase();
            var visible = q ? items.filter(function (item) { return matches(item, q); }) : items;
            clear(list);
            if (!items.length) {
                toolbar.hidden = true;
                setState(t('catalog_empty'));
                moreWrap.hidden = true;
                return;
            }
            toolbar.hidden = false;
            if (!visible.length) setState(t('no_results'));
            else state.hidden = true;
            visible.slice(0, shown).forEach(function (item) { list.appendChild(card(item)); });
            moreWrap.hidden = visible.length <= shown;
        }

        function card(item) {
            var article = el('article', { 'class': 'card extension-card market-card', id: 'market-' + item.slug });
            // Without a screenshot the card looks like a theme card; a
            // screenshot that fails to load is removed the same way.
            // With a market page, the title and the screenshot open it; the
            // screenshot link repeats the title's, so it is skipped by
            // keyboards and screen readers.
            var listing = safeHTTPS(item.listing_url);
            var shot = null;
            var src = safeHTTPS(item.screenshot_url);
            if (src) {
                shot = listing
                    ? el('a', { 'class': 'market-shot', href: listing, target: '_blank', rel: 'noopener noreferrer', tabindex: '-1', 'aria-hidden': 'true' })
                    : el('div', { 'class': 'market-shot' });
                var img = el('img', { src: src, alt: '', loading: 'lazy', decoding: 'async', referrerpolicy: 'no-referrer' });
                img.addEventListener('error', function () {
                    if (shot.parentNode) shot.parentNode.removeChild(shot);
                });
                shot.appendChild(img);
            }

            var badges = el('div', { 'class': 'extension-card-badges' }, [el('span', { 'class': 'extension-version', text: 'v' + item.latest.version })]);
            if (item.installed && item.installed.status === 'revoked') badges.appendChild(badge('badge-danger', t('revoked')));
            else if (item.update) badges.appendChild(badge('badge-update', t('update_available')));
            else if (item.installed) badges.appendChild(badge('badge-active', t('installed')));

            var content = el('div', { 'class': 'extension-card-content' }, [
                el('div', { 'class': 'extension-card-heading' }, [
                    listing
                        ? el('h3', { title: item.title }, el('a', { 'class': 'market-title-link', href: listing, target: '_blank', rel: 'noopener noreferrer', text: item.title }))
                        : el('h3', { text: item.title, title: item.title }),
                    badges
                ]),
                item.author ? el('p', { 'class': 'market-author', text: t('by_author', item.author) }) : null,
                item.summary ? el('p', { 'class': 'extension-desc market-summary', text: item.summary }) : null
            ]);

            var actions = el('div', { 'class': 'extension-actions' });
            if (!item.latest.compatible) {
                actions.appendChild(el('button', { type: 'button', 'class': 'btn btn-secondary btn-sm btn-blocked', disabled: true, text: t('incompatible', item.latest.core || '') }));
            } else if (!item.installed || item.update) {
                if (can.install) {
                    var install = el('button', { type: 'button', 'class': 'btn btn-primary btn-sm',
                        text: item.installed ? t('upgrade_to', item.latest.version) : t('install') });
                    install.addEventListener('click', function () { openInstall(item, install); });
                    actions.appendChild(install);
                }
            }
            var meta = el('div', { 'class': 'extension-meta' }, [
                el('span', { 'class': 'extension-slug', text: item.slug, title: item.slug }),
                listing ? el('a', { href: listing, target: '_blank', rel: 'noopener noreferrer', 'class': 'market-listing', text: t('view_listing') }) : null
            ]);
            append(article, [shot, el('div', { 'class': 'extension-card-main' }, content), el('div', { 'class': 'extension-card-footer' }, [actions, meta])]);
            return article;
        }

        function openInstall(item, opener) {
            var upgrade = !!item.installed;
            var version = item.latest.version + (upgrade ? ' (' + t('installed_version', item.installed.version) + ')' : '');
            openDialog({
                opener: opener,
                title: t(upgrade ? 'upgrade_title' : 'install_title', item.title),
                body: [
                    details([
                        [t('detail_version'), version],
                        item.author ? [t('detail_author'), item.author] : null,
                        [t('detail_publisher'), item.latest.publisher_kid, 'ext-mono'],
                        [t('detail_size'), formatSize(item.latest.size)],
                        [t('detail_requires'), requiresText(item.latest.requires)]
                    ]),
                    el('p', { 'class': 'ext-dialog-label', text: t('external_help') }),
                    hostList(item.latest.external),
                    el('p', { 'class': 'ext-muted', text: t('no_scripts') })
                ],
                submitLabel: t(upgrade ? 'confirm_upgrade' : 'confirm_install'),
                busyLabel: t('installing'),
                onSubmit: function () {
                    var form = new FormData();
                    form.append('slug', item.slug);
                    form.append('version', item.latest.version);
                    return request('POST', '/admin/extensions/themes/install', form).then(function (data) {
                        if (!data.ok) return { error: errorText(data.error) };
                        item.installed = { version: item.latest.version, status: 'ready', signed: true, source: 'market' };
                        item.update = false;
                        var old = document.getElementById('market-' + item.slug);
                        if (old) old.parentNode.replaceChild(card(item), old);
                        installedNotice(item.title, item.latest.version);
                        return true;
                    });
                }
            });
        }

        search.addEventListener('input', function () {
            shown = PAGE_SIZE;
            render();
        });
        more.addEventListener('click', function () {
            shown += PAGE_SIZE;
            render();
        });
        load();
    }

    function initInstalled() {
        var state = market.querySelector('[data-ext-state]');
        var list = market.querySelector('[data-ext-list]');

        function setState(content) {
            clear(state);
            append(state, content);
            state.hidden = false;
        }

        function load() {
            request('GET', '/admin/extensions/themes').then(function (data) {
                clear(list);
                if (!data.ok) {
                    var retry = el('button', { type: 'button', 'class': 'btn btn-secondary btn-sm', text: t('retry') });
                    retry.addEventListener('click', load);
                    setState([el('p', { text: errorText(data.error) }), retry]);
                    return;
                }
                var packages = data.packages || [];
                if (!packages.length) {
                    var links = [el('a', { href: '/admin/themes/market', 'class': 'btn btn-secondary btn-sm', text: t('tab_browse') })];
                    setState([el('p', { text: t('installed_empty') }), el('div', { 'class': 'market-empty-actions' }, links)]);
                    return;
                }
                state.hidden = true;
                packages.forEach(function (pkg) { list.appendChild(packageRow(pkg)); });
                revealHash();
            });
        }

        function revealHash() {
            if (!location.hash) return;
            var target = document.getElementById(location.hash.slice(1));
            if (!target) return;
            var versions = target.querySelector('details');
            if (versions) versions.open = true;
            target.scrollIntoView({ block: 'start' });
        }

        function updateBadge(slug, update) {
            var urgent = update.severity === 'security' || update.severity === 'unsupported';
            return el('a', {
                href: '/admin/themes/market#market-' + encodeURIComponent(slug),
                'class': 'badge badge-update' + (urgent ? ' badge-update-urgent' : ''),
                text: t(update.severity === 'security' ? 'update_version_security' : 'update_version', update.latest_version)
            });
        }

        function statusBadge(status) {
            if (status === 'broken') return badge('badge-danger', t('broken'));
            if (status === 'revoked') return badge('badge-danger', t('revoked'));
            return null;
        }

        function sourceBadge(source) {
            return badge('badge-package', source === 'market' ? t('source_market') : t('source_upload'));
        }

        function packageRow(pkg) {
            var current = null;
            pkg.versions.forEach(function (v) { if (v.current) current = v; });
            var row = el('section', { 'class': 'package-row', id: 'package-' + pkg.slug, 'aria-labelledby': 'package-title-' + pkg.slug });
            var badges = el('div', { 'class': 'package-badges' });
            if (current) {
                append(badges, [
                    el('span', { 'class': 'extension-version', text: 'v' + current.version }),
                    sourceBadge(current.source),
                    current.signed ? null : badge('badge-danger', t('unsigned')),
                    statusBadge(current.status)
                ]);
            }
            if (pkg.update) {
                badges.appendChild(updateBadge(pkg.slug, pkg.update));
                var notes = safeHTTPS(pkg.update.release_url);
                if (notes) badges.appendChild(el('a', { href: notes, target: '_blank', rel: 'noopener noreferrer', 'class': 'package-notes-link', text: t('release_notes') }));
            }
            if (pkg.active) badges.appendChild(badge('badge-active', t('in_use')));

            var head = el('div', { 'class': 'package-head' }, [el('h3', { id: 'package-title-' + pkg.slug, text: pkg.slug }), badges]);
            var notes = el('div', { 'class': 'package-notes' }, [
                pkg.preview ? el('p', { text: t('preview_overrides') }) : null,
                pkg.issue ? el('p', { 'class': 'package-issue', text: t('not_loaded', pkg.issue) }) : null
            ]);

            var actions = el('div', { 'class': 'package-actions' });
            if (can.remove) {
                var uninstall = el('button', { type: 'button', 'class': 'btn btn-secondary btn-sm package-uninstall', text: t('uninstall') });
                if (pkg.active) {
                    uninstall.disabled = true;
                    uninstall.title = t('uninstall_in_use');
                } else {
                    uninstall.addEventListener('click', function () { confirmUninstall(pkg, uninstall); });
                }
                actions.appendChild(uninstall);
            }
            if (pkg.active && can.remove) actions.appendChild(el('small', { 'class': 'ext-muted', text: t('uninstall_in_use') }));

            var versions = el('ul', { 'class': 'package-versions-list' }, pkg.versions.map(function (v) {
                var item = el('li', { 'class': 'package-version' + (v.current ? ' package-version-current' : '') });
                var info = el('div', { 'class': 'package-version-info' }, [
                    el('span', { 'class': 'extension-version', text: 'v' + v.version }),
                    v.current ? badge('badge-active', t('current_version')) : null,
                    sourceBadge(v.source),
                    v.signed ? null : badge('badge-danger', t('unsigned')),
                    statusBadge(v.status),
                    el('span', { 'class': 'ext-muted', text: t('installed_at', formatDate(v.installed_at)) })
                ]);
                item.appendChild(info);
                if (can.install && !v.current && v.status === 'ready') {
                    var rollback = el('button', { type: 'button', 'class': 'btn btn-secondary btn-sm', text: t('rollback') });
                    rollback.addEventListener('click', function () { confirmRollback(pkg, v, rollback); });
                    item.appendChild(rollback);
                }
                return item;
            }));
            var disclosure = el('details', { 'class': 'package-versions' }, [el('summary', { text: t('versions', pkg.versions.length) }), versions]);
            append(row, [head, notes, disclosure, actions]);
            return row;
        }

        function confirmRollback(pkg, version, opener) {
            openDialog({
                opener: opener,
                title: t('rollback_title', pkg.slug),
                body: [el('p', { text: t('rollback_confirm', pkg.slug, version.version) })],
                submitLabel: t('rollback'),
                busyLabel: t('working'),
                onSubmit: function () {
                    var form = new FormData();
                    form.append('id', String(version.id));
                    return request('POST', '/admin/extensions/themes/rollback', form).then(function (data) {
                        if (!data.ok) return { error: errorText(data.error) };
                        notice('success', t('rolled_back', pkg.slug, version.version));
                        load();
                        return true;
                    });
                }
            });
        }

        function confirmUninstall(pkg, opener) {
            openDialog({
                opener: opener,
                danger: true,
                title: t('uninstall_title', pkg.slug),
                body: [el('p', { text: t('uninstall_help') })],
                submitLabel: t('uninstall'),
                busyLabel: t('working'),
                onSubmit: function () {
                    var form = new FormData();
                    form.append('slug', pkg.slug);
                    return request('POST', '/admin/extensions/themes/uninstall', form).then(function (data) {
                        if (!data.ok) return { error: errorText(data.error) };
                        notice('success', t('uninstalled', pkg.slug));
                        load();
                        return true;
                    });
                }
            });
        }

        market.reloadInstalled = load;
        load();
    }

    // ---------------------------------------------------------------
    // Upload, and the confirmation of an unsigned package
    // ---------------------------------------------------------------

    function upload(file, confirmDigest) {
        var form = new FormData();
        form.append('package', file);
        if (confirmDigest) form.append('confirm_digest', confirmDigest);
        return request('POST', '/admin/extensions/themes/upload', form);
    }

    function uploaded(result) {
        var name = result.summary && result.summary.name ? result.summary.name : result.slug;
        var version = result.package ? result.package.version : '';
        installedNotice(name, version);
        if (market && typeof market.reloadInstalled === 'function') market.reloadInstalled();
    }

    function problemList(problems) {
        return el('ul', { 'class': 'ext-problems' }, problems.map(function (p) {
            var where = p.path ? p.path + (p.line ? ':' + p.line : '') : '';
            return el('li', null, [el('code', { text: p.rule }), where ? ' ' + where : '', ' — ' + p.message]);
        }));
    }

    function devModeLink() {
        if (!can.developer) return null;
        return el('a', { href: '/admin/settings#developer-options', text: t('devmode_title') });
    }

    function openUpload(event) {
        var opener = event.currentTarget;
        var input = el('input', { type: 'file', id: 'extUploadFile', accept: '.zip,application/zip' });
        openDialog({
            opener: opener,
            title: t('upload_title'),
            body: [
                el('p', { text: t('upload_help') }),
                el('div', { 'class': 'form-group' }, [el('label', { 'for': 'extUploadFile', text: t('upload_file') }), input])
            ],
            submitLabel: t('upload_submit'),
            busyLabel: t('uploading'),
            onSubmit: function (ctx) {
                var file = input.files && input.files[0];
                if (!file) return { error: t('upload_choose') };
                return upload(file, '').then(function (data) {
                    if (data.ok) {
                        uploaded(data.result);
                        return true;
                    }
                    var err = data.error || {};
                    if (err.code === 'confirmation_required' && data.package) {
                        ctx.close();
                        openUnsigned(file, data.package, opener);
                        return false;
                    }
                    if (err.code === 'check_failed' && data.problems) {
                        return { error: [el('p', { text: t('check_problems') }), problemList(data.problems)] };
                    }
                    if (err.code === 'unsigned_not_allowed') {
                        return { error: [t('unsigned_needs_devmode'), ' ', devModeLink()] };
                    }
                    return { error: errorText(err) };
                });
            }
        });
    }

    function openUnsigned(file, pkg, opener) {
        var signature = has('signature.' + pkg.signature) ? t('signature.' + pkg.signature) : pkg.signature;
        openDialog({
            opener: opener,
            danger: true,
            title: t('unsigned_title'),
            body: [
                el('p', { 'class': 'ext-warning-lead', text: t('unsigned_lead') }),
                details([
                    [t('detail_name'), pkg.name + ' (' + pkg.slug + ')'],
                    [t('detail_version'), pkg.version],
                    [t('detail_digest'), pkg.digest, 'ext-mono'],
                    [t('detail_source'), t('source_manual')],
                    [t('detail_signature'), signature],
                    [t('detail_files'), t('files_size', pkg.files, formatSize(pkg.size))]
                ]),
                el('p', { 'class': 'ext-dialog-label', text: t('external_help') }),
                hostList(pkg.external),
                el('p', { 'class': 'ext-risk', text: t('unsigned_risks') })
            ],
            ack: t('unsigned_ack'),
            submitLabel: t('unsigned_submit'),
            busyLabel: t('installing'),
            onSubmit: function () {
                return upload(file, pkg.digest).then(function (data) {
                    if (data.ok) {
                        uploaded(data.result);
                        return true;
                    }
                    var err = data.error || {};
                    if (err.code === 'check_failed' && data.problems) {
                        return { error: [el('p', { text: t('check_problems') }), problemList(data.problems)] };
                    }
                    return { error: errorText(err) };
                });
            }
        });
    }

    // ---------------------------------------------------------------
    // Developer options on the settings page
    // ---------------------------------------------------------------

    function initDeveloperMode(block) {
        var errorBox = block.querySelector('[data-ext-devmode-error]');
        var on = block.querySelector('[data-ext-devmode-on]');
        var off = block.querySelector('[data-ext-devmode-off]');

        function reload() {
            window.location.hash = 'developer-options';
            window.location.reload();
        }

        if (on && !on.disabled) {
            on.addEventListener('click', function () {
                openDialog({
                    opener: on,
                    danger: true,
                    title: t('devmode_dialog_title'),
                    body: [el('ul', { 'class': 'ext-risk-list' }, ['devmode_risk_1', 'devmode_risk_2', 'devmode_risk_3', 'devmode_risk_4'].map(function (key) {
                        return el('li', { text: t(key) });
                    }))],
                    ack: t('devmode_ack'),
                    password: t('devmode_password'),
                    submitLabel: t('devmode_submit'),
                    busyLabel: t('working'),
                    onSubmit: function (ctx) {
                        var form = new FormData();
                        form.append('enabled', '1');
                        form.append('confirm', '1');
                        form.append('password', ctx.password());
                        ctx.clearPassword();
                        return request('POST', '/admin/settings/developer-mode', form).then(function (data) {
                            if (!data.ok) return { error: errorText(data.error) };
                            reload();
                            return true;
                        });
                    }
                });
            });
        }
        if (off) {
            off.addEventListener('click', function () {
                off.disabled = true;
                errorBox.hidden = true;
                var form = new FormData();
                form.append('enabled', '0');
                request('POST', '/admin/settings/developer-mode', form).then(function (data) {
                    if (data.ok) {
                        reload();
                        return;
                    }
                    off.disabled = false;
                    errorBox.textContent = errorText(data.error);
                    errorBox.hidden = false;
                });
            });
        }
    }

    // ---------------------------------------------------------------

    if (market) {
        if (market.getAttribute('data-tab') === 'installed') initInstalled();
        else if (market.getAttribute('data-market-enabled') === 'true') initBrowse();
    }
    document.querySelectorAll('[data-ext-upload]').forEach(function (button) {
        button.addEventListener('click', openUpload);
    });
    var devmode = document.querySelector('[data-ext-devmode]');
    if (devmode) initDeveloperMode(devmode);
})();
