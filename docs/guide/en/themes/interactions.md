# Theme Interactions

GoPress ships a front-end script with eight interactions themes commonly need: toggle, navigation submenus, dialog, carousel, scroll state and back to top, reveal, reading progress, and copy. A theme configures them with `data-gp-*` attributes in its templates and writes no script.

[Theme packages](theme-packages.md) cannot contain scripts, so this is how they build menus, dialogs and carousels. Go themes can use it too.

## Loading the script

Call `{{uiScript}}` in `layouts/base.tmpl`, either in `<head>` or before `</body>`:

```gotemplate
<head>
  …
  {{uiScript}}
</head>
```

It outputs something like:

```html
<script src="/static/gopress/ui.js?v=3f9a1c0b7e2d" defer></script>
```

- The script comes with GoPress and is served by your site, so the Content-Security-Policy for theme packages already allows it. Do not declare it in `[[external]]`.
- The `?v=` value is a digest of the script. It changes whenever the script does, so browsers never use a stale copy.
- A theme that does not call `{{uiScript}}` does not load the script, and nothing changes on its pages.
- GoPress reserves `/static/gopress/`. A theme's files under `static/gopress/` cannot be requested, and the package check warns about them with `file.shadowed`.

## Conventions

### Configuration and state attributes

| Kind | Form | Written by |
|---|---|---|
| Configuration | `data-gp-<component>`, `data-gp-<component>-<option>` | The theme's templates; the script only reads them |
| State | `data-gp-is-<state>`, such as `data-gp-is-open` | The script; the theme styles them and does not write them |

Components only change state: they set `data-gp-is-*`, `aria-*`, `hidden` and `inert` attributes and the `--gp-progress` CSS variable. The theme's CSS decides everything visual: showing, hiding and animating.

### Attribute values

A configuration attribute holds one of:

- **an element ID**: a letter, then letters, digits, `-` or `_`, up to 64 characters;
- **a whole number** in the range the attribute allows;
- **fixed words**, separated by spaces.

When a value is wrong, the script logs a warning starting with `GoPress UI:` to the browser console:

- a wrong attribute that points at a target (such as `data-gp-toggle`) turns that component off;
- a wrong option (such as `data-gp-interval`) falls back to its default.

### IDs must be unique

HTML in posts and comments may carry `id` attributes. So that user content cannot stand in for a theme's element, components only use an ID that **exactly one element** on the page has. When two elements share an ID, components pointing at it do nothing and log a warning.

Prefix your theme's IDs, for example `qr-site-nav`, so they do not clash with heading anchors in posts.

Content sanitizing removes `data-gp-*` attributes from user content, so authors and commenters cannot trigger these components.

### Page state

The script sets two attributes on `<html>`:

| Attribute | Meaning | Use |
|---|---|---|
| `data-gp-ready` | The script has run | Prefix styles that must not hide anything without the script, such as reveals |
| `data-gp-is-locked` | A modal dialog is open | Stop the page from scrolling: `html[data-gp-is-locked] { overflow: hidden; }` |

To style the page by an element's state, use `:has()`:

```css
body:has(#qr-site-nav[data-gp-is-open]) { overflow: hidden; }
```

### Without the script

The components are enhancements; pages should work without the script:

- give a link that opens a dialog a real URL (such as the sign-in page); the script only stops the navigation when it takes over;
- open submenus on wide screens with CSS `:hover` and `:focus-within`;
- prefix the hidden state of reveals with `html[data-gp-ready]`.

Plugin output may use these attributes too, but only as an enhancement: the active theme may not load the script.

### Reduced motion

When a visitor has turned on reduced motion (`prefers-reduced-motion: reduce`):

- carousels do not play;
- back to top jumps instead of scrolling smoothly;
- reveals show their elements at once.

Put your theme's own transitions inside `@media (prefers-reduced-motion: no-preference)` as well.

## Components

### 1. Toggle `data-gp-toggle`

For mobile menus, search drawers, comment replies and "show more".

```gotemplate
<button type="button" data-gp-toggle="qr-nav" aria-label="{{T .Ctx "menu"}}">☰</button>
<nav id="qr-nav" data-gp-dismiss="escape outside link">…</nav>

<button type="button" data-gp-toggle="qr-reply-{{.ID}}">{{T .Ctx "reply"}}</button>
<form id="qr-reply-{{.ID}}" hidden>
  <textarea data-gp-autofocus></textarea>
  <button type="button" data-gp-toggle="qr-reply-{{.ID}}">{{T .Ctx "cancel"}}</button>
</form>
```

- A click on a toggle opens or closes its target:
  - an open target has `data-gp-is-open`;
  - a target that has `hidden` when the page loads also has `hidden` switched.
- Every toggle for the same target gets `aria-controls` and `aria-expanded`, kept in step.
- On opening, the element in the target with `data-gp-autofocus` gets focus.
- `data-gp-dismiss` on the target adds ways to close it, one or more of:

  | Word | Closes on |
  |---|---|
  | `escape` | Esc |
  | `outside` | A click outside the target and its toggles |
  | `link` | A click on a link inside the target |

- When focus was inside a target that closes, it goes back to the toggle that opened it; after a link closes it, focus stays with the link.
- `data-gp-group="<name>"` on targets keeps only one target of the group open, for a row of dropdowns.

```css
#qr-nav:not([data-gp-is-open]) { display: none; }
@media (min-width: 900px) { #qr-nav { display: block; } }
```

### 2. Navigation submenus `data-gp-menu`

On the navigation list:

```html
<ul data-gp-menu data-gp-menu-below="900">
  <li><a href="/docs">Docs</a>
    <ul>…</ul>
  </li>
  <li><a href="/blog">Blog</a><button type="button" aria-label="Show the Blog submenu">▾</button>
    <ul>…</ul>
  </li>
</ul>
```

- **Narrow screens** (viewport narrower than `data-gp-menu-below`, a number from 320 to 2000, default 900):
  - a click on an item's trigger opens or closes its sublist; the `li` gets `data-gp-is-open` and the trigger `aria-expanded`;
  - the trigger is a `<button>` directly inside the `li`, otherwise its first link, which then does not navigate on narrow screens. If the parent page must stay reachable, add a button, or repeat the link in the sublist;
  - closing an item closes the items open inside it;
  - Esc with focus in a submenu closes it and puts focus on its trigger.
- **Wide screens**: the script leaves clicks alone and the theme's CSS opens submenus on `:hover` and `:focus-within`. When the screen becomes wide, the script clears the state it set.
- A sublist must be a `<ul>` or `<ol>` directly inside the `li`. Plugin output with the same structure, such as a language switcher, works too.
- Clicks with Ctrl, ⌘, Shift or Alt are left to the browser (to open a new tab, for example).

```css
[data-gp-menu] li > ul { display: none; }
[data-gp-menu] li[data-gp-is-open] > ul { display: block; }
@media (min-width: 900px) {
  [data-gp-menu] li:hover > ul,
  [data-gp-menu] li:focus-within > ul { display: block; }
}
```

If a hover submenu sits apart from its parent on wide screens, bridge the gap with a transparent `::before` or use `padding-top`, or the menu closes while the pointer crosses the gap.

### 3. Dialog `data-gp-dialog`

For sign-in forms, side drawers and image lightboxes. **Only the native `<dialog>` element is supported.**

```gotemplate
<a href="{{loginURL .Ctx}}" data-gp-dialog-open="qr-login" aria-haspopup="dialog">{{T .Ctx "sign_in"}}</a>

<dialog id="qr-login" data-gp-dialog data-gp-dismiss="outside" aria-labelledby="qr-login-title">
  <h2 id="qr-login-title">{{T .Ctx "sign_in"}}</h2>
  <input name="email" type="email" data-gp-autofocus>
  <button type="button" data-gp-dialog-close aria-label="{{T .Ctx "close"}}">×</button>
</dialog>
```

- A click on `data-gp-dialog-open` opens the dialog as a modal. The browser provides the `::backdrop`, closes it on Esc, and makes the rest of the page inert.
- The script adds:
  - focus on `data-gp-autofocus` when it opens;
  - `data-gp-is-locked` on `<html>` while it is open;
  - focus back on the element that opened it after it closes.
- `data-gp-dialog-close` closes the dialog it is in.
- `data-gp-dismiss`:
  - `outside` closes it on a click on the backdrop;
  - `link` closes it on a click on a link inside it, for a navigation drawer.

  Esc is handled by the browser and always works.
- With a toggle open inside the dialog, Esc closes the toggle first, then the dialog.
- When the dialog is missing, or the target is not a `<dialog>` with `data-gp-dialog`, the link navigates as usual.

### 4. Carousel `data-gp-carousel`

For hero slides, testimonials and project galleries.

```gotemplate
<section data-gp-carousel data-gp-interval="6000" aria-roledescription="{{T .Ctx "carousel"}}" aria-label="{{T .Ctx "featured"}}">
  {{range .Slides}}<div data-gp-slide>…</div>{{end}}
  <button type="button" data-gp-carousel-prev aria-label="{{T .Ctx "previous"}}">‹</button>
  <button type="button" data-gp-carousel-next aria-label="{{T .Ctx "next"}}">›</button>
  {{range $i, $s := .Slides}}
  <button type="button" data-gp-carousel-to="{{$i}}" aria-label="{{$s.Title}}"></button>
  {{end}}
  <button type="button" data-gp-carousel-pause aria-label="{{T .Ctx "pause"}}">❙❙</button>
</section>
```

- The current slide has `data-gp-is-active`; the others have `aria-hidden="true"` and `inert`. The carousel starts on the slide marked `data-gp-is-active` in the markup, otherwise on the first.
- Previous and next wrap around. `data-gp-carousel-to="<index>"` (from 0) goes to a slide, and the button for the current slide has `aria-current="true"`. Gallery thumbnails are such buttons.
- With focus on one of the carousel's buttons, the left and right arrow keys change slides. With focus in a slide's content they do nothing.
- `data-gp-interval` plays the carousel: milliseconds, 0 or 2000 to 60000, default 0 (no autoplay). Autoplay:
  - holds while the pointer is over the carousel or focus is in it;
  - starts paused when the visitor has turned on reduced motion;
  - does not change slides while the page is in the background;
  - stops for good when the visitor presses the pause button, which switches `aria-pressed`.
- The theme writes the dots, buttons and their accessible names; the script creates none. Transitions (fade, slide) are the theme's CSS:

```css
[data-gp-slide] { opacity: 0; transition: opacity .4s; position: absolute; inset: 0; }
[data-gp-slide][data-gp-is-active] { opacity: 1; position: relative; }
[data-gp-carousel-to][aria-current="true"] { background: var(--accent); }
```

### 5. Scroll state and back to top

```gotemplate
<header data-gp-scroll="48">…</header>
<button type="button" data-gp-scroll="400" data-gp-to-top aria-label="{{T .Ctx "back_to_top"}}">↑</button>
```

- `data-gp-scroll="<pixels>"` (0 to 5000): once the page has scrolled further, the element has `data-gp-is-past`. Use it to shrink a header or show a back-to-top button.
- `data-gp-to-top` scrolls back to the top on click and moves focus to `<main>` (the script gives it `tabindex="-1"`).

```css
.site-header[data-gp-is-past] { box-shadow: 0 1px 8px rgb(0 0 0 / .08); }
.to-top:not([data-gp-is-past]) { visibility: hidden; opacity: 0; }
```

### 6. Reveal `data-gp-reveal`

The element gets `data-gp-is-visible` the first time it enters the viewport, and keeps it. Prefix the hidden state with `html[data-gp-ready]` so content shows without the script:

```css
@media (prefers-reduced-motion: no-preference) {
  html[data-gp-ready] [data-gp-reveal]:not([data-gp-is-visible]) { opacity: 0; transform: translateY(16px); }
  [data-gp-reveal] { transition: opacity .5s, transform .5s; }
}
```

With reduced motion turned on, or without `IntersectionObserver`, elements show at once. All of them show before printing.

### 7. Reading progress `data-gp-progress`

The script sets the CSS variable `--gp-progress`, from 0 to 1, on the element:

- with an empty value it measures the whole page;
- with an element ID it measures that element: 0 when its top reaches the top of the viewport, 1 when its bottom is in view.

```gotemplate
<div class="progress" data-gp-progress="qr-article" aria-hidden="true"></div>
<article id="qr-article">…</article>
```

```css
.progress { position: fixed; inset: 0 0 auto; height: 3px; background: var(--accent);
  transform-origin: left; transform: scaleX(var(--gp-progress, 0)); }
```

A progress bar is decoration; give it `aria-hidden="true"`.

### 8. Copy `data-gp-copy`

```gotemplate
<input id="qr-share-url" value="{{.CanonicalURL}}" readonly>
<button type="button" data-gp-copy="qr-share-url" data-gp-copy-done="qr-copied">{{T .Ctx "copy_link"}}</button>
<span role="status"><span id="qr-copied" hidden>{{T .Ctx "copied"}}</span></span>
```

- A click copies the source to the clipboard: the value of an input or textarea, otherwise the element's text without surrounding white space.
- On success the button has `data-gp-is-done`, and the note `data-gp-copy-done` names loses `hidden`, both for 3 seconds. Keep the note inside an element with `role="status"` that is always there, so screen readers announce it.
- When the browser does not allow clipboard access (for example without HTTPS), the script selects the source so the visitor can copy it.

## Interactions built with HTML and CSS

These need no script:

| Need | How |
|---|---|
| Smooth scrolling to anchors | `@media (prefers-reduced-motion: no-preference) { html { scroll-behavior: smooth; } }` |
| Form validation | `required`, `type="email"`, `pattern`; style errors with `:user-invalid` |
| Mega menus and hover dropdowns | `:hover`, `:focus-within`, with a transparent `::before` bridge or `transition-delay` |
| Accordions and FAQs | `<details>`; `<details name="faq">` keeps one of a group open |
| Footnotes and small hints | The `popover` attribute with a `popovertarget` button |
| Horizontal lists | `overflow-x: auto` with `scroll-snap-type` |
| Sharing to social sites | Plain links with `target="_blank" rel="noopener"` |

There is no tabs component yet; use `<details>`, or toggles with `data-gp-group`.

## What the components do not do

- They create no elements or text: nothing like `innerHTML`. Dots, buttons and messages are written by the theme, in its own translations.
- They never use an attribute value as code or as a URL, and never read link addresses.
- They make no requests and use no cookies or local storage.
- They act only on `data-gp-*` attributes written in theme templates.

## Browser support

Chrome, Edge, Firefox and Safari (including iOS) released in the last three years. The script uses the native `<dialog>`, `inert`, `IntersectionObserver` and the clipboard API.

Older browsers raise no errors; the interactions just do not happen. Make sure navigation still works then, for example with wide-screen menus that open on `:hover` and `:focus-within`.

## Attribute reference

| Attribute | On | Value |
|---|---|---|
| `data-gp-toggle` | Toggle | Target ID |
| `data-gp-dismiss` | Toggle target, dialog | `escape`, `outside`, `link`, combined |
| `data-gp-group` | Toggle target | Group name |
| `data-gp-autofocus` | Element in a target or dialog | None |
| `data-gp-menu` | Navigation list | None |
| `data-gp-menu-below` | Navigation list | 320–2000, default 900 |
| `data-gp-dialog` | `<dialog>` | None |
| `data-gp-dialog-open` | Link or button | Dialog ID |
| `data-gp-dialog-close` | Button in a dialog | None |
| `data-gp-carousel` | Carousel container | None |
| `data-gp-interval` | Carousel container | 0 or 2000–60000, default 0 |
| `data-gp-slide` | Slide | None |
| `data-gp-carousel-prev`, `-next`, `-pause` | Button in a carousel | None |
| `data-gp-carousel-to` | Button in a carousel | Slide index from 0 |
| `data-gp-scroll` | Any element | 0–5000 |
| `data-gp-to-top` | Button | None |
| `data-gp-reveal` | Any element | None |
| `data-gp-progress` | Progress bar | Empty, or an element ID |
| `data-gp-copy` | Button | Source element ID |
| `data-gp-copy-done` | Copy button | Note element ID |

| State attribute | Appears on |
|---|---|
| `data-gp-ready`, `data-gp-is-locked` | `<html>` |
| `data-gp-is-open` | An open toggle target; an open `li` on narrow screens |
| `data-gp-is-active` | The current slide |
| `data-gp-is-past` | An element the page has scrolled past |
| `data-gp-is-visible` | A revealed element |
| `data-gp-is-done` | A copy button that just copied |
