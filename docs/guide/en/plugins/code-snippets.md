# Code Snippets Plugin

The `code-snippets` plugin provides WPCode-like site-level HTML and JavaScript injection. It is useful for analytics, tag managers, verification snippets, chat widgets, heatmaps, or small operational scripts.

## Slots

The plugin writes snippets into standard frontend hook slots:

| Slot | Output location |
|---|---|
| `theme.head.end` | Before `</head>`. |
| `theme.body.open` | Immediately after `<body>`. |
| `theme.footer.end` | Before `</body>`, after theme scripts. |

Themes must declare these hooks in `layouts/base.tmpl` for the plugin to work.

## Admin UI

The plugin settings page exposes three text areas:

- Head snippets.
- Body-open snippets.
- Footer snippets.

Saving the settings updates plugin options and clears frontend cache.

## Safety Notes

The plugin intentionally stores raw snippets because its purpose is code injection. Only trusted administrators should have access to this settings page. Avoid using it for large application logic; create a plugin or theme change instead.

