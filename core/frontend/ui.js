// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

// GoPress front-end interactions for themes.
//
// A theme loads this script with {{uiScript}} and configures it with
// data-gp-* attributes in its templates (docs/guide/en/themes/interactions.md).
// Components only change state: data-gp-is-* attributes, aria-* attributes,
// hidden, inert, tabindex on <main>, and the --gp-progress custom property.
// They never create elements or text, never read URLs from attributes and
// never make requests; the theme's CSS does all the styling. Clicks and keys
// are delegated to the document, so markup inserted later works too;
// carousels, scroll states, reveals and progress bars are set up once when
// the page loads.
(function () {
  'use strict';

  const root = document.documentElement;
  if (root.gopressUI) {
    return;
  }
  root.gopressUI = true;

  // ---- Attribute values ----------------------------------------------------

  const ID = /^[A-Za-z][A-Za-z0-9_-]{0,63}$/;
  const warned = new WeakMap();

  // warn reports a configuration mistake once per element and attribute.
  function warn(el, attr, message) {
    let seen = warned.get(el);
    if (!seen) {
      seen = new Set();
      warned.set(el, seen);
    }
    if (seen.has(attr)) {
      return;
    }
    seen.add(attr);
    if (window.console && console.warn) {
      console.warn('GoPress UI: ' + attr + ' ' + message, el);
    }
  }

  // idAttr returns the element ID an attribute holds, or null when the
  // attribute is missing or does not hold an ID.
  function idAttr(el, attr) {
    const value = el.getAttribute(attr);
    if (value === null) {
      return null;
    }
    if (!ID.test(value)) {
      warn(el, attr, 'must be an element ID');
      return null;
    }
    return value;
  }

  // byId returns the element with an ID only when exactly one element has
  // it. Posts and comments may carry id attributes; when one repeats an ID
  // the theme uses, nothing is returned, so user content cannot stand in for
  // the theme's element.
  function byId(id, from, attr) {
    const found = document.querySelectorAll('[id="' + id + '"]');
    if (found.length === 1) {
      return found[0];
    }
    warn(from, attr, found.length ? 'names an ID that ' + found.length + ' elements share' : 'names no element');
    return null;
  }

  // target returns the element an ID attribute points to, or null.
  function target(el, attr) {
    const id = idAttr(el, attr);
    return id ? byId(id, el, attr) : null;
  }

  // intAttr returns the whole number an attribute holds, or def when the
  // attribute is missing. A value outside [min, max] warns and gives def.
  function intAttr(el, attr, min, max, def) {
    const value = el.getAttribute(attr);
    if (value === null) {
      return def;
    }
    if (/^\d{1,6}$/.test(value)) {
      const n = parseInt(value, 10);
      if (n >= min && n <= max) {
        return n;
      }
    }
    warn(el, attr, 'must be a whole number from ' + min + ' to ' + max);
    return def;
  }

  // wordsAttr returns the set of words an attribute holds. An unknown word
  // warns and gives an empty set.
  function wordsAttr(el, attr, allowed) {
    const words = new Set();
    const value = el.getAttribute(attr);
    if (value === null) {
      return words;
    }
    for (const word of value.split(/\s+/)) {
      if (!word) {
        continue;
      }
      if (allowed.indexOf(word) < 0) {
        warn(el, attr, 'allows only: ' + allowed.join(', '));
        return new Set();
      }
      words.add(word);
    }
    return words;
  }

  function dismissOf(el) {
    return wordsAttr(el, 'data-gp-dismiss', ['escape', 'outside', 'link']);
  }

  // ---- Shared helpers ------------------------------------------------------

  const motion = window.matchMedia ? window.matchMedia('(prefers-reduced-motion: reduce)') : null;

  function reducedMotion() {
    return Boolean(motion && motion.matches);
  }

  function isTyping(el) {
    return Boolean(el && (el.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(el.tagName)));
  }

  function setFlag(el, name, on) {
    if (on) {
      el.setAttribute(name, '');
    } else {
      el.removeAttribute(name);
    }
  }

  // own returns the elements matching selector inside a component root that
  // belong to it rather than to a component of the same kind nested in it.
  function own(rootEl, selector, kind) {
    return Array.prototype.filter.call(rootEl.querySelectorAll(selector), (el) => el.closest(kind) === rootEl);
  }

  function focusElement(el) {
    if (el && el.isConnected && typeof el.focus === 'function') {
      el.focus();
    }
  }

  // Scroll and resize work runs at most once per animation frame.
  const scrollTasks = [];
  const resizeTasks = [];
  let framePending = false;
  let resized = false;

  function runFrame() {
    framePending = false;
    if (resized) {
      resized = false;
      resizeTasks.forEach((task) => task());
    }
    scrollTasks.forEach((task) => task());
  }

  function requestFrame(event) {
    if (event && event.type === 'resize') {
      resized = true;
    }
    if (!framePending) {
      framePending = true;
      window.requestAnimationFrame(runFrame);
    }
  }

  // ---- 1. Toggle: data-gp-toggle -------------------------------------------

  const toggleState = new WeakMap();
  const openTargets = [];

  // stateOf remembers, the first time a target is seen, whether it is
  // shown with the hidden attribute or styled with data-gp-is-open alone.
  function stateOf(el) {
    let state = toggleState.get(el);
    if (!state) {
      state = { usesHidden: el.hasAttribute('hidden'), opener: null };
      toggleState.set(el, state);
    }
    return state;
  }

  function isOpen(el) {
    return el.hasAttribute('data-gp-is-open');
  }

  function togglesFor(el) {
    return document.querySelectorAll('[data-gp-toggle="' + el.id + '"]');
  }

  function syncToggles(el) {
    const expanded = isOpen(el) ? 'true' : 'false';
    togglesFor(el).forEach((toggle) => {
      toggle.setAttribute('aria-controls', el.id);
      toggle.setAttribute('aria-expanded', expanded);
    });
  }

  function openTarget(el, opener) {
    if (isOpen(el)) {
      return;
    }
    const group = el.getAttribute('data-gp-group');
    if (group) {
      openTargets.slice().forEach((other) => {
        if (other !== el && other.getAttribute('data-gp-group') === group) {
          closeTarget(other);
        }
      });
    }
    const state = stateOf(el);
    state.opener = opener;
    if (state.usesHidden) {
      el.removeAttribute('hidden');
    }
    el.setAttribute('data-gp-is-open', '');
    openTargets.push(el);
    syncToggles(el);
    focusElement(el.querySelector('[data-gp-autofocus]'));
  }

  // closeTarget closes a target. When focus was inside it, focus goes back
  // to the toggle that opened it, unless keepFocus is set: a followed link
  // decides where focus goes itself.
  function closeTarget(el, keepFocus) {
    if (!isOpen(el)) {
      return;
    }
    const state = stateOf(el);
    const hadFocus = !keepFocus && el.contains(document.activeElement);
    el.removeAttribute('data-gp-is-open');
    if (state.usesHidden) {
      el.setAttribute('hidden', '');
    }
    const at = openTargets.indexOf(el);
    if (at >= 0) {
      openTargets.splice(at, 1);
    }
    syncToggles(el);
    if (hadFocus) {
      const back = state.opener && state.opener.isConnected ? state.opener : togglesFor(el)[0];
      focusElement(back);
    }
  }

  function onToggle(event, toggle) {
    const el = target(toggle, 'data-gp-toggle');
    if (!el) {
      return false;
    }
    event.preventDefault();
    stateOf(el);
    if (isOpen(el)) {
      closeTarget(el);
    } else {
      openTarget(el, toggle);
    }
    return true;
  }

  function initToggles() {
    document.querySelectorAll('[data-gp-toggle]').forEach((toggle) => {
      const el = target(toggle, 'data-gp-toggle');
      if (!el) {
        return;
      }
      stateOf(el);
      if (isOpen(el) && openTargets.indexOf(el) < 0) {
        openTargets.push(el);
      }
      syncToggles(el);
    });
  }

  // dismissTargets closes open targets that a click outside them, or on a
  // link inside them, should close.
  function dismissTargets(clicked, consumed) {
    const link = consumed ? null : clicked.closest('a[href]');
    openTargets.slice().forEach((el) => {
      const dismiss = dismissOf(el);
      if (dismiss.has('outside') && !el.contains(clicked) && !clicked.closest('[data-gp-toggle="' + el.id + '"]')) {
        closeTarget(el);
      } else if (dismiss.has('link') && link && el.contains(link)) {
        closeTarget(el, true);
      }
    });
  }

  // ---- 2. Navigation submenus: data-gp-menu --------------------------------

  const menuNarrow = new WeakMap();

  function isNarrow(menu) {
    return window.innerWidth < intAttr(menu, 'data-gp-menu-below', 320, 2000, 900);
  }

  function sublistOf(li) {
    for (let child = li.firstElementChild; child; child = child.nextElementSibling) {
      if (child.tagName === 'UL' || child.tagName === 'OL') {
        return child;
      }
    }
    return null;
  }

  // triggerOf returns the control that opens an item's sublist: a button
  // directly inside the item, otherwise the item's first link.
  function triggerOf(li) {
    let link = null;
    for (let child = li.firstElementChild; child; child = child.nextElementSibling) {
      if (child.tagName === 'BUTTON') {
        return child;
      }
      if (child.tagName === 'A' && !link) {
        link = child;
      }
    }
    return link;
  }

  function setItem(li, open) {
    setFlag(li, 'data-gp-is-open', open);
    const trigger = triggerOf(li);
    if (trigger) {
      trigger.setAttribute('aria-expanded', open ? 'true' : 'false');
    }
    if (!open) {
      li.querySelectorAll('li[data-gp-is-open]').forEach((inner) => setItem(inner, false));
    }
  }

  function onMenu(event, clicked) {
    const trigger = clicked.closest('a, button');
    const li = trigger && trigger.parentElement;
    if (!li || li.tagName !== 'LI' || !sublistOf(li) || triggerOf(li) !== trigger) {
      return false;
    }
    const menu = li.closest('[data-gp-menu]');
    if (!menu || !isNarrow(menu)) {
      return false;
    }
    event.preventDefault();
    setItem(li, !li.hasAttribute('data-gp-is-open'));
    return true;
  }

  // syncMenus sets aria-expanded on submenu triggers below a menu's
  // breakpoint and clears the component's state above it, where the theme's
  // CSS opens submenus on hover and focus.
  function syncMenus() {
    document.querySelectorAll('[data-gp-menu]').forEach((menu) => {
      const narrow = isNarrow(menu);
      if (menuNarrow.get(menu) === narrow) {
        return;
      }
      menuNarrow.set(menu, narrow);
      menu.querySelectorAll('li').forEach((li) => {
        const trigger = sublistOf(li) && triggerOf(li);
        if (!trigger || li.closest('[data-gp-menu]') !== menu) {
          return;
        }
        if (narrow) {
          trigger.setAttribute('aria-expanded', li.hasAttribute('data-gp-is-open') ? 'true' : 'false');
        } else {
          li.removeAttribute('data-gp-is-open');
          trigger.removeAttribute('aria-expanded');
        }
      });
    });
  }

  // closeMenuAround closes the open submenu that holds focus, below the
  // menu's breakpoint, and puts focus on its trigger.
  function closeMenuAround(focused, scope) {
    const li = focused && focused.closest ? focused.closest('li[data-gp-is-open]') : null;
    const menu = li && li.closest('[data-gp-menu]');
    if (!menu || !isNarrow(menu) || (scope && !scope.contains(li))) {
      return false;
    }
    setItem(li, false);
    focusElement(triggerOf(li));
    return true;
  }

  // ---- 3. Dialog: data-gp-dialog -------------------------------------------

  const openDialogs = [];
  const dialogOpeners = new WeakMap();

  function topDialog() {
    for (let i = openDialogs.length - 1; i >= 0; i--) {
      if (openDialogs[i].open) {
        return openDialogs[i];
      }
    }
    return null;
  }

  // onDialogOpen opens the dialog a trigger names. When there is none, the
  // click goes ahead, so a link to the same form on its own page still works.
  function onDialogOpen(event, opener) {
    const dialog = target(opener, 'data-gp-dialog-open');
    if (!dialog) {
      return false;
    }
    if (dialog.tagName !== 'DIALOG' || !dialog.hasAttribute('data-gp-dialog') || typeof dialog.showModal !== 'function') {
      warn(opener, 'data-gp-dialog-open', 'must name a <dialog data-gp-dialog>');
      return false;
    }
    if (!dialog.open) {
      try {
        dialog.showModal();
      } catch (err) {
        return false;
      }
      dialogOpeners.set(dialog, opener);
      openDialogs.push(dialog);
      root.setAttribute('data-gp-is-locked', '');
      focusElement(dialog.querySelector('[data-gp-autofocus]'));
    }
    event.preventDefault();
    return true;
  }

  function onDialogClose(event, control) {
    const dialog = control.closest('dialog');
    if (!dialog || !dialog.open) {
      return false;
    }
    event.preventDefault();
    dialog.close();
    return true;
  }

  // onDialogBackdrop closes a dialog with data-gp-dismiss="outside" when the
  // click lands on its backdrop: the dialog itself is the target, outside
  // its box.
  function onDialogBackdrop(event, clicked) {
    if (clicked.tagName !== 'DIALOG' || openDialogs.indexOf(clicked) < 0 || !dismissOf(clicked).has('outside')) {
      return false;
    }
    const box = clicked.getBoundingClientRect();
    const inside = event.clientX >= box.left && event.clientX <= box.right &&
      event.clientY >= box.top && event.clientY <= box.bottom;
    if (inside) {
      return false;
    }
    clicked.close();
    return true;
  }

  function dismissDialogOnLink(clicked) {
    const link = clicked.closest('a[href]');
    const dialog = link && link.closest('dialog');
    if (dialog && dialog.open && openDialogs.indexOf(dialog) >= 0 && dismissOf(dialog).has('link')) {
      dialog.close();
    }
  }

  // A dialog closes by the close control, the backdrop, Esc or a form with
  // method="dialog"; the close event covers all of them.
  function onDialogClosed(event) {
    const dialog = event.target;
    const at = openDialogs.indexOf(dialog);
    if (at < 0) {
      return;
    }
    openDialogs.splice(at, 1);
    if (!topDialog()) {
      root.removeAttribute('data-gp-is-locked');
    }
    const opener = dialogOpeners.get(dialog);
    dialogOpeners.delete(dialog);
    focusElement(opener);
  }

  // ---- 4. Carousel: data-gp-carousel ---------------------------------------

  const carousels = new WeakMap();
  const CAROUSEL = '[data-gp-carousel]';

  function slidesOf(el) {
    return own(el, '[data-gp-slide]', CAROUSEL);
  }

  function showSlide(state, index) {
    const slides = slidesOf(state.el);
    const count = slides.length;
    if (!count) {
      return;
    }
    state.index = ((index % count) + count) % count;
    slides.forEach((slide, i) => {
      const active = i === state.index;
      setFlag(slide, 'data-gp-is-active', active);
      setFlag(slide, 'inert', !active);
      if (active) {
        slide.removeAttribute('aria-hidden');
      } else {
        slide.setAttribute('aria-hidden', 'true');
      }
    });
    own(state.el, '[data-gp-carousel-to]', CAROUSEL).forEach((button) => {
      if (intAttr(button, 'data-gp-carousel-to', 0, 999, -1) === state.index) {
        button.setAttribute('aria-current', 'true');
      } else {
        button.removeAttribute('aria-current');
      }
    });
  }

  // schedule (re)starts autoplay when nothing holds it: the interval is
  // set, the visitor has not paused it, and the pointer and focus are
  // elsewhere.
  function schedule(state) {
    window.clearTimeout(state.timer);
    state.timer = 0;
    if (state.interval > 0 && !state.paused && !state.hovered && !state.focused) {
      state.timer = window.setTimeout(() => {
        if (!document.hidden) {
          showSlide(state, state.index + 1);
        }
        schedule(state);
      }, state.interval);
    }
  }

  function syncPause(state) {
    own(state.el, '[data-gp-carousel-pause]', CAROUSEL).forEach((button) => {
      button.setAttribute('aria-pressed', state.paused ? 'true' : 'false');
    });
  }

  function initCarousel(el) {
    const slides = slidesOf(el);
    if (!slides.length) {
      warn(el, 'data-gp-carousel', 'has no data-gp-slide');
      return;
    }
    let interval = intAttr(el, 'data-gp-interval', 0, 60000, 0);
    if (interval > 0 && interval < 2000) {
      warn(el, 'data-gp-interval', 'must be 0 or from 2000 to 60000');
      interval = 0;
    }
    const state = {
      el: el,
      index: 0,
      interval: interval,
      timer: 0,
      // Reduced motion starts paused; the pause control can still start it.
      paused: interval === 0 || reducedMotion(),
      hovered: false,
      focused: false,
    };
    carousels.set(el, state);
    const current = slides.findIndex((slide) => slide.hasAttribute('data-gp-is-active'));
    showSlide(state, current < 0 ? 0 : current);
    syncPause(state);
    if (interval > 0) {
      el.addEventListener('mouseenter', () => {
        state.hovered = true;
        schedule(state);
      });
      el.addEventListener('mouseleave', () => {
        state.hovered = false;
        schedule(state);
      });
      el.addEventListener('focusin', () => {
        state.focused = true;
        schedule(state);
      });
      el.addEventListener('focusout', (event) => {
        if (!el.contains(event.relatedTarget)) {
          state.focused = false;
          schedule(state);
        }
      });
    }
    schedule(state);
  }

  function onCarousel(event, clicked) {
    const control = clicked.closest('[data-gp-carousel-prev], [data-gp-carousel-next], [data-gp-carousel-to], [data-gp-carousel-pause]');
    const el = control && control.closest(CAROUSEL);
    const state = el && carousels.get(el);
    if (!state) {
      return false;
    }
    event.preventDefault();
    if (control.hasAttribute('data-gp-carousel-pause')) {
      state.paused = !state.paused;
      syncPause(state);
    } else if (control.hasAttribute('data-gp-carousel-to')) {
      const to = intAttr(control, 'data-gp-carousel-to', 0, 999, -1);
      if (to >= 0) {
        showSlide(state, to);
      }
    } else {
      showSlide(state, state.index + (control.hasAttribute('data-gp-carousel-next') ? 1 : -1));
    }
    schedule(state);
    return true;
  }

  // Arrow keys change slides while focus is on the carousel's controls (not
  // inside a slide, whose content becomes inert when it changes).
  function onCarouselKey(event) {
    const focused = document.activeElement;
    if (!focused || isTyping(focused) || focused.closest('[data-gp-slide]')) {
      return;
    }
    const el = focused.closest(CAROUSEL);
    const state = el && carousels.get(el);
    if (!state) {
      return;
    }
    let forward = event.key === 'ArrowRight';
    if (window.getComputedStyle(el).direction === 'rtl') {
      forward = !forward;
    }
    event.preventDefault();
    showSlide(state, state.index + (forward ? 1 : -1));
    schedule(state);
  }

  // ---- 5. Scroll state and back to top: data-gp-scroll, data-gp-to-top -----

  function initScroll() {
    const items = [];
    document.querySelectorAll('[data-gp-scroll]').forEach((el) => {
      const past = intAttr(el, 'data-gp-scroll', 0, 5000, -1);
      if (past >= 0) {
        items.push({ el: el, past: past });
      }
    });
    if (items.length) {
      scrollTasks.push(() => {
        const y = window.scrollY;
        items.forEach((item) => setFlag(item.el, 'data-gp-is-past', y > item.past));
      });
    }
  }

  function onToTop(event, control) {
    event.preventDefault();
    if (reducedMotion()) {
      window.scrollTo(0, 0);
    } else {
      window.scrollTo({ top: 0, behavior: 'smooth' });
    }
    const main = document.querySelector('main');
    if (main) {
      if (!main.hasAttribute('tabindex')) {
        main.setAttribute('tabindex', '-1');
      }
      main.focus({ preventScroll: true });
    } else if (document.activeElement === control) {
      control.blur();
    }
    return true;
  }

  // ---- 6. Reveal: data-gp-reveal -------------------------------------------

  function initReveal() {
    const items = document.querySelectorAll('[data-gp-reveal]');
    if (!items.length) {
      return;
    }
    const showAll = () => items.forEach((el) => el.setAttribute('data-gp-is-visible', ''));
    if (reducedMotion() || !('IntersectionObserver' in window)) {
      showAll();
      return;
    }
    const observer = new IntersectionObserver((entries) => {
      entries.forEach((entry) => {
        if (entry.isIntersecting) {
          entry.target.setAttribute('data-gp-is-visible', '');
          observer.unobserve(entry.target);
        }
      });
    });
    items.forEach((el) => observer.observe(el));
    window.addEventListener('beforeprint', showAll);
  }

  // ---- 7. Reading progress: data-gp-progress -------------------------------

  // progressOf returns how far the page, or the element of, has been read:
  // 0 before it, 1 once its end is in view.
  function progressOf(of) {
    const view = window.innerHeight;
    if (!of) {
      const span = root.scrollHeight - view;
      return span > 0 ? Math.min(1, Math.max(0, window.scrollY / span)) : 1;
    }
    const box = of.getBoundingClientRect();
    const span = box.height - view;
    if (span <= 0) {
      return box.bottom <= view ? 1 : 0;
    }
    return Math.min(1, Math.max(0, -box.top / span));
  }

  function initProgress() {
    const bars = [];
    document.querySelectorAll('[data-gp-progress]').forEach((el) => {
      let of = null;
      if (el.getAttribute('data-gp-progress') !== '') {
        of = target(el, 'data-gp-progress');
        if (!of) {
          return;
        }
      }
      bars.push({ el: el, of: of });
    });
    if (bars.length) {
      scrollTasks.push(() => {
        bars.forEach((bar) => bar.el.style.setProperty('--gp-progress', progressOf(bar.of).toFixed(4)));
      });
    }
  }

  // ---- 8. Copy: data-gp-copy -----------------------------------------------

  const copyTimers = new WeakMap();

  function onCopy(event, button) {
    const source = target(button, 'data-gp-copy');
    if (!source) {
      return false;
    }
    event.preventDefault();
    const isField = source.tagName === 'INPUT' || source.tagName === 'TEXTAREA';
    const text = isField ? source.value : source.textContent.trim();
    const select = () => {
      if (isField) {
        source.focus();
        source.select();
      } else if (window.getSelection) {
        window.getSelection().selectAllChildren(source);
      }
    };
    if (window.isSecureContext && navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(text).then(() => copied(button), select);
    } else {
      select();
    }
    return true;
  }

  // copied marks the button for three seconds and shows the note that
  // data-gp-copy-done names for as long.
  function copied(button) {
    const note = button.hasAttribute('data-gp-copy-done') ? target(button, 'data-gp-copy-done') : null;
    button.setAttribute('data-gp-is-done', '');
    if (note) {
      note.removeAttribute('hidden');
    }
    window.clearTimeout(copyTimers.get(button));
    copyTimers.set(button, window.setTimeout(() => {
      button.removeAttribute('data-gp-is-done');
      if (note) {
        note.setAttribute('hidden', '');
      }
    }, 3000));
  }

  // ---- Events --------------------------------------------------------------

  function onClick(event) {
    let clicked = event.target;
    if (clicked && clicked.nodeType !== 1) {
      clicked = clicked.parentElement;
    }
    if (!clicked || !clicked.closest) {
      return;
    }
    // A modified click on a link opens it elsewhere; leave it to the browser.
    const modified = event.metaKey || event.ctrlKey || event.shiftKey || event.altKey;
    let consumed = false;
    let el;
    if (!modified && (el = clicked.closest('[data-gp-dialog-open]'))) {
      onDialogOpen(event, el);
    } else if ((el = clicked.closest('[data-gp-dialog-close]'))) {
      onDialogClose(event, el);
    } else if (onDialogBackdrop(event, clicked)) {
      return;
    } else if (!modified && onMenu(event, clicked)) {
      consumed = true;
    } else if ((el = clicked.closest('[data-gp-toggle]'))) {
      consumed = onToggle(event, el);
    } else if (onCarousel(event, clicked)) {
      consumed = true;
    } else if ((el = clicked.closest('[data-gp-to-top]'))) {
      onToTop(event, el);
    } else if ((el = clicked.closest('[data-gp-copy]'))) {
      onCopy(event, el);
    }
    if (!consumed) {
      dismissDialogOnLink(clicked);
    }
    dismissTargets(clicked, consumed);
  }

  function onEscape(event) {
    // With a dialog open, only what is inside it is in front.
    const scope = topDialog();
    if (closeMenuAround(document.activeElement, scope)) {
      event.preventDefault();
      return;
    }
    // The target holding focus closes first, otherwise the latest opened.
    const candidates = openTargets.filter((el) => (!scope || scope.contains(el)) && dismissOf(el).has('escape'));
    const focused = candidates.filter((el) => el.contains(document.activeElement));
    const el = focused.length ? focused[focused.length - 1] : candidates[candidates.length - 1];
    if (el) {
      closeTarget(el);
      event.preventDefault();
    }
  }

  function onKeydown(event) {
    if (event.defaultPrevented) {
      return;
    }
    if (event.key === 'Escape' || event.key === 'Esc') {
      onEscape(event);
    } else if (event.key === 'ArrowLeft' || event.key === 'ArrowRight') {
      onCarouselKey(event);
    }
  }

  function init() {
    initToggles();
    syncMenus();
    document.querySelectorAll(CAROUSEL).forEach(initCarousel);
    initScroll();
    initReveal();
    initProgress();
    resizeTasks.push(syncMenus);

    document.addEventListener('click', onClick);
    document.addEventListener('keydown', onKeydown);
    // close does not bubble; listening in the capture phase still sees it.
    document.addEventListener('close', onDialogClosed, true);
    window.addEventListener('scroll', requestFrame, { passive: true });
    window.addEventListener('resize', requestFrame);
    window.addEventListener('load', requestFrame);

    runFrame();
    root.setAttribute('data-gp-ready', '');
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', init);
  } else {
    init();
  }
})();
