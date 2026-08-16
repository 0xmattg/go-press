/* Communa theme — front-end interactions */
(function () {
    'use strict';

    // ---- Mobile navigation ----
    var toggle = document.getElementById('cmn-nav-toggle');
    var nav = document.querySelector('[data-site-nav]');
    if (toggle && nav) {
        toggle.addEventListener('click', function () {
            var open = nav.classList.toggle('is-open');
            toggle.classList.toggle('is-open', open);
            toggle.setAttribute('aria-expanded', open ? 'true' : 'false');
        });
        // Close after tapping a leaf link (not a submenu parent).
        nav.querySelectorAll('.cmn-nav-menu a').forEach(function (link) {
            link.addEventListener('click', function () {
                var li = link.parentElement;
                if (li && li.querySelector('ul') && window.matchMedia('(max-width: 900px)').matches) {
                    return; // parent handled below
                }
                nav.classList.remove('is-open');
                toggle.classList.remove('is-open');
                toggle.setAttribute('aria-expanded', 'false');
            });
        });
    }

    // ---- Generic submenu expansion (e.g. an injected language switcher) ----
    // On mobile, top-level items that carry a child <ul> expand in place with a
    // correct aria-expanded state instead of relying on hover.
    document.querySelectorAll('.cmn-nav-menu > li').forEach(function (li) {
        var sub = li.querySelector(':scope > ul');
        if (!sub) return;
        var trigger = li.querySelector(':scope > a');
        if (!trigger) return;
        trigger.setAttribute('aria-haspopup', 'true');
        trigger.setAttribute('aria-expanded', 'false');
        li.addEventListener('mouseenter', function () { trigger.setAttribute('aria-expanded', 'true'); });
        li.addEventListener('mouseleave', function () { trigger.setAttribute('aria-expanded', 'false'); });
        trigger.addEventListener('click', function (e) {
            if (!window.matchMedia('(max-width: 900px)').matches) return;
            e.preventDefault();
            var open = li.classList.toggle('is-open');
            trigger.setAttribute('aria-expanded', open ? 'true' : 'false');
        });
    });

    // ---- Sticky header shadow ----
    var header = document.getElementById('cmn-header');
    if (header) {
        var onScrollHeader = function () {
            header.classList.toggle('is-scrolled', window.scrollY > 8);
        };
        onScrollHeader();
        window.addEventListener('scroll', onScrollHeader, { passive: true });
    }

    // ---- Scroll to top ----
    var top = document.getElementById('cmn-scroll-top');
    if (top) {
        window.addEventListener('scroll', function () {
            top.classList.toggle('is-visible', window.scrollY > 480);
        }, { passive: true });
        top.addEventListener('click', function () {
            window.scrollTo({ top: 0, behavior: 'smooth' });
        });
    }

    // ---- Comment reply toggles ----
    document.querySelectorAll('[data-comment-reply]').forEach(function (btn) {
        btn.addEventListener('click', function () {
            var form = document.getElementById(btn.getAttribute('data-comment-reply'));
            if (!form) return;
            form.hidden = false;
            var field = form.querySelector('textarea');
            if (field) field.focus();
        });
    });
    document.querySelectorAll('[data-comment-cancel]').forEach(function (btn) {
        btn.addEventListener('click', function () {
            var form = document.getElementById(btn.getAttribute('data-comment-cancel'));
            if (form) form.hidden = true;
        });
    });

    // ---- Featured carousel ----
    document.querySelectorAll('[data-carousel]').forEach(function (root) {
        var track = root.querySelector('[data-carousel-track]');
        var slides = track ? Array.prototype.slice.call(track.children) : [];
        var dotsWrap = root.querySelector('[data-carousel-dots]');
        var prevBtn = root.querySelector('[data-carousel-prev]');
        var nextBtn = root.querySelector('[data-carousel-next]');
        if (!track || slides.length <= 1) {
            if (dotsWrap) dotsWrap.style.display = 'none';
            if (prevBtn) prevBtn.style.display = 'none';
            if (nextBtn) nextBtn.style.display = 'none';
            return;
        }
        var interval = Math.max(2500, parseInt(root.getAttribute('data-interval') || '6000', 10) || 6000);
        var index = 0;
        var timer;
        var dots = [];

        slides.forEach(function (_, i) {
            var dot = document.createElement('button');
            dot.type = 'button';
            dot.className = 'cmn-carousel-dot' + (i === 0 ? ' is-active' : '');
            dot.setAttribute('aria-label', 'Go to slide ' + (i + 1));
            dot.addEventListener('click', function () { go(i); restart(); });
            dotsWrap.appendChild(dot);
            dots.push(dot);
        });

        function go(i) {
            index = (i + slides.length) % slides.length;
            track.style.transform = 'translateX(' + (-index * 100) + '%)';
            dots.forEach(function (d, j) { d.classList.toggle('is-active', j === index); });
        }
        function restart() {
            clearInterval(timer);
            timer = setInterval(function () { go(index + 1); }, interval);
        }

        if (nextBtn) nextBtn.addEventListener('click', function () { go(index + 1); restart(); });
        if (prevBtn) prevBtn.addEventListener('click', function () { go(index - 1); restart(); });
        root.addEventListener('mouseenter', function () { clearInterval(timer); });
        root.addEventListener('mouseleave', restart);
        restart();
    });

    // ---- Login modal ----
    var loginModal = document.querySelector('[data-login-modal]');
    var loginDialog = loginModal ? loginModal.querySelector('[role="dialog"]') : null;
    var loginTrigger = null;
    function closeLogin() {
        if (!loginModal || loginModal.hidden) return;
        loginModal.hidden = true;
        document.body.classList.remove('cmn-modal-open');
        if (loginTrigger) loginTrigger.focus();
    }
    function openLogin(trigger) {
        if (!loginModal || !loginDialog) return;
        loginTrigger = trigger;
        loginModal.hidden = false;
        document.body.classList.add('cmn-modal-open');
        loginDialog.focus();
    }
    document.querySelectorAll('[data-login-open]').forEach(function (opener) {
        opener.addEventListener('click', function (e) {
            if (!loginModal) return;
            e.preventDefault();
            openLogin(opener);
        });
    });
    if (loginModal) {
        loginModal.querySelectorAll('[data-login-close]').forEach(function (closer) {
            closer.addEventListener('click', closeLogin);
        });
        loginModal.addEventListener('keydown', function (e) {
            if (e.key === 'Escape') { e.preventDefault(); closeLogin(); return; }
            if (e.key !== 'Tab' || !loginDialog) return;
            var focusable = Array.prototype.slice.call(loginDialog.querySelectorAll('a[href], button:not([disabled])'));
            if (!focusable.length) { e.preventDefault(); loginDialog.focus(); return; }
            var first = focusable[0];
            var last = focusable[focusable.length - 1];
            if (e.shiftKey && (document.activeElement === first || document.activeElement === loginDialog)) { e.preventDefault(); last.focus(); }
            else if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first.focus(); }
        });
    }

    // ---- Contact form: lightweight client-side guard ----
    var contact = document.getElementById('cmn-contact-form');
    if (contact) {
        contact.addEventListener('submit', function (e) {
            var name = contact.querySelector('#name');
            var email = contact.querySelector('#email');
            var message = contact.querySelector('#message');
            var errors = [];
            if (name && !name.value.trim()) errors.push(name);
            if (email && (!email.value.trim() || !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email.value))) errors.push(email);
            if (message && !message.value.trim()) errors.push(message);
            if (errors.length) {
                e.preventDefault();
                errors[0].focus();
            }
        });
    }
})();
