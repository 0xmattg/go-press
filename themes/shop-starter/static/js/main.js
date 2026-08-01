(() => {
  const body = document.body;
  const header = document.querySelector('[data-site-header]');
  const toggle = document.querySelector('[data-nav-toggle]');

  const closeNav = () => {
    body.classList.remove('ss-nav-open');
    toggle?.setAttribute('aria-expanded', 'false');
    document.querySelectorAll('[data-site-nav] > ul > li.is-submenu-open').forEach((item) => {
      item.classList.remove('is-submenu-open');
      item.querySelector(':scope > a')?.setAttribute('aria-expanded', 'false');
    });
  };

  toggle?.addEventListener('click', () => {
    const open = body.classList.toggle('ss-nav-open');
    toggle.setAttribute('aria-expanded', String(open));
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
      if (!window.matchMedia('(max-width: 860px)').matches) return;
      event.preventDefault();
      const open = link.parentElement.classList.toggle('is-submenu-open');
      link.setAttribute('aria-expanded', String(open));
    });
  });

  document.querySelectorAll('[data-site-nav] > ul > li > ul a').forEach((link) => link.addEventListener('click', closeNav));
  document.addEventListener('keydown', (event) => { if (event.key === 'Escape') closeNav(); });
  window.addEventListener('resize', () => { if (window.innerWidth > 860) closeNav(); });

  const loginModal = document.querySelector('[data-login-modal]');
  const loginDialog = loginModal?.querySelector('[role="dialog"]');
  const loginOpeners = document.querySelectorAll('[data-login-open]');
  let loginTrigger = null;

  const closeLogin = () => {
    if (!loginModal || loginModal.hidden) return;
    loginModal.hidden = true;
    body.classList.remove('ss-modal-open');
    loginTrigger?.focus();
  };

  const openLogin = (trigger) => {
    if (!loginModal || !loginDialog) return;
    closeNav();
    loginTrigger = trigger;
    loginModal.hidden = false;
    body.classList.add('ss-modal-open');
    loginDialog.focus();
  };

  loginOpeners.forEach((opener) => opener.addEventListener('click', (event) => {
    if (!loginModal) return;
    event.preventDefault();
    openLogin(opener);
  }));
  loginModal?.querySelectorAll('[data-login-close]').forEach((closer) => closer.addEventListener('click', closeLogin));

  loginModal?.addEventListener('keydown', (event) => {
    if (event.key === 'Escape') {
      event.preventDefault();
      closeLogin();
      return;
    }
    if (event.key !== 'Tab' || !loginDialog) return;
    const focusable = [...loginDialog.querySelectorAll('a[href], button:not([disabled])')];
    if (!focusable.length) {
      event.preventDefault();
      loginDialog.focus();
      return;
    }
    const first = focusable[0];
    const last = focusable[focusable.length - 1];
    if (event.shiftKey && (document.activeElement === first || document.activeElement === loginDialog)) {
      event.preventDefault();
      last.focus();
    } else if (!event.shiftKey && document.activeElement === last) {
      event.preventDefault();
      first.focus();
    }
  });

  const updateHeader = () => header?.classList.toggle('is-scrolled', window.scrollY > 8);
  updateHeader();
  window.addEventListener('scroll', updateHeader, { passive: true });
})();
