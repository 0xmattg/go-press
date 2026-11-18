# Code Snippets Plugin

The `code-snippets` plugin provides WPCode-like site-level HTML and JavaScript injection. It is useful for analytics, tag managers, verification snippets, chat widgets, heatmaps, or small operational scripts.

## Slots

The plugin writes snippets into standard frontend hook slots:

| Slot | Output location |
|---|---|
| `theme.head.end` | Before `</head>`. |
| `theme.body.open` | Immediately after `<body>`. |
| `theme.footer.end` | Before `</body>`, after theme scripts. |

The three values are stored as site options:

| Slot | Option key |
|---|---|
| `theme.head.end` | `plugin_code-snippets_head` |
| `theme.body.open` | `plugin_code-snippets_body_open` |
| `theme.footer.end` | `plugin_code-snippets_footer` |

Themes must declare these hooks in `layouts/base.tmpl` for the plugin to work.

```gotemplate
<head>
  ...
  {{renderHook "theme.head.end" .}}
</head>
<body>
  {{renderHook "theme.body.open" .}}
  ...
  <script src="/static/js/main.js"></script>
  {{renderHook "theme.footer.end" .}}
</body>
```

The plugin does not scan or post-process final HTML. Each slot must appear once
at its semantic location.

## Admin UI

The plugin settings page exposes three text areas:

- Head snippets.
- Body-open snippets.
- Footer snippets.

Saving the settings updates plugin options and clears frontend cache.

On activation the plugin registers three filters and retains their
`hook.Handle` values. Deactivation removes all three; stored snippets remain in
the options table but stop rendering immediately.

## Theme Packages

While a theme package is the active theme, pages carry a
Content-Security-Policy that runs only scripts the site knows about. The
plugin declares the snippets' scripts automatically:

- `<script src="https://…">` allows that script's host;
- an inline `<script>` is allowed by the hash of its exact text, so editing
  a snippet updates it on the next request;
- when any snippet runs a script, requests to `https:` hosts are allowed, for
  analytics and widgets.

Under a theme package these do not run: inline event handler attributes such
as `onclick=`, code that needs `eval` (for example Tag Manager custom
JavaScript variables), and scripts that a snippet's script loads from other
hosts. Themes built into GoPress have no such policy.

## Safety Notes

The plugin intentionally stores raw snippets because its purpose is code injection. Only trusted administrators should have access to this settings page. Avoid using it for large application logic; create a plugin or theme change instead.

Third-party scripts can affect performance, privacy compliance, and frontend
behavior. Validate them in a non-production environment and do not paste code
from an untrusted source.
