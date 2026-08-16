(() => {
    const body = document.body;
    const navToggle = document.querySelector('[data-nav-toggle]');
    const searchToggle = document.querySelector('[data-search-open]');
    const searchDrawer = document.querySelector('[data-search-drawer]');
    const progress = document.querySelector('.mj-reading-progress span');

    const closeNav = () => {
        body.classList.remove('nav-open');
        navToggle?.setAttribute('aria-expanded', 'false');
        document.querySelectorAll('[data-site-nav] > ul > li.is-submenu-open').forEach((item) => {
            item.classList.remove('is-submenu-open');
            item.querySelector(':scope > a')?.setAttribute('aria-expanded', 'false');
        });
    };

    navToggle?.addEventListener('click', () => {
        const open = body.classList.toggle('nav-open');
        body.classList.remove('search-open');
        if (searchDrawer) searchDrawer.hidden = true;
        navToggle.setAttribute('aria-expanded', String(open));
    });

    searchToggle?.addEventListener('click', () => {
        const open = searchDrawer?.hidden ?? true;
        closeNav();
        if (!searchDrawer) return;
        searchDrawer.hidden = !open;
        body.classList.toggle('search-open', open);
        if (open) window.setTimeout(() => searchDrawer.querySelector('input')?.focus(), 40);
    });

    document.addEventListener('keydown', (event) => {
        if (event.key !== 'Escape') return;
        closeNav();
        body.classList.remove('search-open');
        if (searchDrawer) searchDrawer.hidden = true;
    });

    document.querySelectorAll('[data-site-nav] > ul > li > a').forEach((link) => {
        const submenu = link.parentElement?.querySelector(':scope > ul');
        if (!submenu) {
            link.addEventListener('click', closeNav);
            return;
        }
        link.setAttribute('aria-haspopup', 'true');
        link.setAttribute('aria-expanded', 'false');
        link.addEventListener('click', (event) => {
            if (!window.matchMedia('(max-width: 980px)').matches) return;
            event.preventDefault();
            const open = link.parentElement.classList.toggle('is-submenu-open');
            link.setAttribute('aria-expanded', String(open));
        });
    });

    document.querySelectorAll('[data-site-nav] > ul > li > ul a').forEach((link) => link.addEventListener('click', closeNav));

    document.querySelectorAll('[data-comment-reply]').forEach((button) => {
        button.addEventListener('click', () => {
            const form = document.getElementById(button.dataset.commentReply || '');
            if (!form) return;
            form.hidden = false;
            form.querySelector('textarea')?.focus();
        });
    });

    document.querySelectorAll('[data-comment-cancel]').forEach((button) => {
        button.addEventListener('click', () => {
            const form = document.getElementById(button.dataset.commentCancel || '');
            if (form) form.hidden = true;
        });
    });

    const loginModal = document.querySelector('[data-login-modal]');
    const loginDialog = loginModal?.querySelector('[role="dialog"]');
    let loginTrigger = null;
    const closeLogin = () => {
        if (!loginModal || loginModal.hidden) return;
        loginModal.hidden = true;
        body.classList.remove('mj-modal-open');
        loginTrigger?.focus();
    };
    const openLogin = (trigger) => {
        if (!loginModal || !loginDialog) return;
        closeNav();
        loginTrigger = trigger;
        loginModal.hidden = false;
        body.classList.add('mj-modal-open');
        loginDialog.focus();
    };
    document.querySelectorAll('[data-login-open]').forEach((opener) => opener.addEventListener('click', (event) => {
        if (!loginModal) return;
        event.preventDefault();
        openLogin(opener);
    }));
    loginModal?.querySelectorAll('[data-login-close]').forEach((closer) => closer.addEventListener('click', closeLogin));
    loginModal?.addEventListener('keydown', (event) => {
        if (event.key === 'Escape') { event.preventDefault(); closeLogin(); return; }
        if (event.key !== 'Tab' || !loginDialog) return;
        const focusable = [...loginDialog.querySelectorAll('a[href], button:not([disabled])')];
        if (!focusable.length) { event.preventDefault(); loginDialog.focus(); return; }
        const first = focusable[0];
        const last = focusable[focusable.length - 1];
        if (event.shiftKey && (document.activeElement === first || document.activeElement === loginDialog)) { event.preventDefault(); last.focus(); }
        else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus(); }
    });

    const updateProgress = () => {
        if (!progress) return;
        const max = document.documentElement.scrollHeight - window.innerHeight;
        const value = max > 0 ? Math.min(100, Math.max(0, window.scrollY / max * 100)) : 0;
        progress.style.width = `${value}%`;
    };
    updateProgress();
    window.addEventListener('scroll', updateProgress, { passive: true });
    window.addEventListener('resize', updateProgress);
})();
