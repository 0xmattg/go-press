# Admin Extension Points

Admin extension points let plugins and themes add behavior to the CMS while keeping the core admin stable.

Every extension point follows the same contract: core defines a stable data
shape, hook/provider name, and trigger location; extensions register an
implementation during activation; with no extension, filters pass through and
actions have no side effects.

## Plugin Settings Pages

A plugin can provide an admin settings page by implementing settings provider interfaces. The admin will show a **Plugin Settings** button on the plugin card and route requests to the plugin-owned template or renderer.

Typical responsibilities:

- Render settings UI.
- Load current settings data.
- Save submitted settings.
- Return translated labels through the admin locale system.

## Content Form Fields

Plugins can add extra fields to content editing pages through `admin.content_form.fields`.

The `seo-extras` plugin uses this hook to append a collapsible SEO panel with fields for SEO title, description, Open Graph image, and robots.

## Content Save Actions

Plugins can listen to `admin.content.saved` and persist additional form values. This keeps plugin data separate from the core content model while still making it part of the editorial workflow.

## Content List Tabs

The admin exposes `admin.HookContentListTabs` for plugins that need additional list filters. The multilingual plugin uses it to add language tabs and counts to content list pages.

Tabs normally compose with the [Content Scope API](../architecture/content-scope.md):
the selected query parameter registers a request scope, and totals, filters, and
pagination all use the scoped query.

## Taxonomy List Tabs

`admin.HookTaxonomyListTabs` exposes the same request-aware tab pattern above
Category, Tag, and other registered taxonomy lists. The filter receives the
current `*gin.Context` and taxonomy type, so an extension can append a URL such
as `?lang=zh` and register the matching [Taxonomy Scope](../architecture/content-scope.md).

Counts, trees, parent choices, content-reference totals, create/update/delete
commands, and content-editor selectors must consume that same scope. A tab is a
navigation control, not an authorization boundary; each protected operation
still requires its specific taxonomy RBAC capability.

## Content Permalink Prefix

`admin.HookContentPermalinkPrefix` lets an extension prepend a contextual
segment such as `/zh` or `/site-2` to the permalink shown in the content editor.
Its value is an empty string by default, and its arguments are the current
`*gin.Context` and content row. The hook changes the editor preview only through
the same core URL contract; themes do not need to know why the prefix exists.

## Dashboard Widgets

Plugins can append trusted dashboard markup through `admin.dashboard.widgets`.
The filter value is `template.HTML`, and the first argument is the dashboard
template root. A widget must check the current role before rendering and protect
every backing API with core authentication plus a specific RBAC capability.

The `gopress-analytics` plugin uses this slot for its traffic summary, while its
JSON endpoint requires `analytics.read`.

## Mail and Notification Hooks

Mail is split into two layers: `core/mail` owns message delivery, while notification rules listen to core events and call the mail service. Plugins can filter outgoing messages, observe delivery results, or customize the default contact-message notification.

| Hook | Type | Purpose |
|---|---|---|
| `content.created` | action | Fired after a content row and its meta are saved. Args: `*content.Content, map[string]string` |
| `mail.message` | filter | Modify `mail.Message` before delivery |
| `mail.before_send` | action | Observe a message before SMTP delivery |
| `mail.sent` | action | Fired after successful delivery |
| `mail.failed` | action | Fired after failed delivery. Args: `mail.Message, error` |
| `notification.contact_message.recipients` | filter | Modify new contact-message recipients, value: `[]string` |
| `notification.contact_message.subject` | filter | Modify the contact-message subject, value: `string` |
| `notification.contact_message.body` | filter | Modify the contact-message plain-text body, value: `string` |

Example: add a sales inbox to contact-message notifications:

```go
e.Hooks.AddFilter(hook.NotificationContactMessageRecipients,
    func(value interface{}, args ...interface{}) interface{} {
        recipients, _ := value.([]string)
        return append(recipients, "sales@example.com")
    }, 20)
```

Plugins that need to send their own notification emails should use core's `mail.Sender` capability instead of reading SMTP settings or depending on a concrete driver:

```go
sender := plugin.MailSender(app)
if sender == nil {
    return
}

err := sender.Send(ctx, mail.Message{
    To:      []string{"admin@example.com"},
    Subject: "Plugin notification",
    Text:    "Something happened.",
})
```

Themes can access the same capability through `theme.App.MailSender()` or `t.MailSender()` when embedding `BaseTheme`, for example in a theme-owned form handler. Themes should still avoid knowing SMTP hosts, Gmail app keys, or whether delivery uses `go-mail` or `stdlib`; the preferred default remains: themes save content or fire core hooks, while notification rules or plugins send mail.

SMTP configuration, notification switches, and delivery behavior stay in core or plugin extension points.

## Frontend Template Slots

The same core hook bus exposes semantic frontend locations:

| Hook | Location |
|---|---|
| `theme.head.end` | Immediately before `</head>`. |
| `theme.body.open` | Immediately after `<body>`. |
| `theme.footer.end` | After theme scripts and before `</body>`. |
| `header.nav.after` | At the end of the primary navigation list. |

Themes declare these locations with `renderHook`; plugins return markup that
matches the surrounding semantics. Extensions must not scan or post-process a
complete HTML response to find an insertion point.

## Translation Requirements

Admin-facing theme and plugin settings should not hard-code Chinese or English in templates. They should use the admin translation helper or component-owned locale files. If a component ships only one language, the admin should fall back to that available language instead of hiding the UI.

## Design Rule

Extensions should communicate with the admin through core hooks, providers, and template functions. Avoid direct imports between themes and plugins, and avoid post-processing full HTML responses to inject admin UI.

Every plugin must retain returned hook handles and remove them in
`Deactivate`. Core rebuilds the router and clears relevant cache paths after
activation changes so middleware, routes, admin UI, and frontend output reflect
the new state without a restart.
