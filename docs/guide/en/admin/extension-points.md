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

Use `engine.Content.SaveMeta(item.ID, key, value)` for content meta. It runs in its own transaction under a lock on the content/key pair: concurrent first saves of a key leave a single row, existing duplicate rows are all updated to the new value, and database errors are returned instead of being mistaken for a missing row. Check the error and report it with `c.Error(err)` so the admin shows a failed save instead of a success notice.

Save order and transaction boundaries:

1. The content row, the meta fields declared by its content type, and its category/tag relationships commit in **one transaction**. If any step fails, everything rolls back and the admin shows the form again with the error, so nothing is left half saved.
2. `admin.content.saved` fires after that transaction commits. Plugin fields are saved there; when they fail, the content itself is already saved, so report the failure with `c.Error(err)` or the admin will show a success notice.
3. A plugin that writes several fields should write them in one transaction (for example `engine.DB.Transaction` with `content.NewRepository(tx)`) so it never saves only some of them.

Extensions that call `CommandService.Create` / `Update` directly can join that transaction with `content.WithTxParticipant(func(tx *gorm.DB, item *content.Content) error)`. Participants run after the content row and meta are written; returning an error rolls the whole save back and fires no content event.

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

## Admin Notices

A plugin can show a notice at the top of every admin page, for example when
its settings need attention, through the `admin.notices` filter. The value is
`[]notice.Notice` (package `core/notice`); the arguments are the request's
`*gin.Context` and the admin language. Append notices with text in that
language; the admin owns the markup, so a plugin never returns HTML.

```go
handle := e.Hooks.AddFilter(hook.AdminNotices, func(v interface{}, args ...interface{}) interface{} {
    list := v.([]notice.Notice)
    if gatewayKeySet() {
        return list
    }
    lang, _ := args[1].(string)
    return append(list, notice.Notice{
        Source:    "commerce",         // your plugin's slug
        ID:        "gateway-key",      // unique within your plugin
        Category:  notice.CategoryConfiguration,
        Severity:  notice.SeverityWarning,
        Title:     t(lang, "gateway.key_missing"),
        Link:      &notice.Link{URL: "/admin/plugins/commerce/settings", Label: t(lang, "gateway.open_settings")},
        Snoozable: true,
        Resource:  "commerce_settings", // who can fix it sees it
        Action:    "update",
    })
}, 10)
```

- **Who sees it** — roles with `Resource.Action`; without them, `notice.read`,
  which only super admins hold by default.
- **Dismissing** — `Snoozable` shows "×", which hides the notice for that
  user for `RemindAfter` (1 hour to 7 days, 24 hours by default);
  `Acknowledgeable` shows "Got it", which hides this `Revision` for that user
  for good. Raise `Revision` when the content changes enough that people must
  read it again. With neither, the notice stays until it expires or you stop
  providing it, which suits a notice that reports a current state.
- **Limits** — plain text only; titles are cut at 120 characters and messages
  at 1,000. A link must be https or an `/admin/` path, or it is dropped. A
  plugin provides at most 10 notices; notices without a valid source, ID or
  title are dropped and logged. Set `ExpiresAt` for a notice that should end on
  its own.
- **Cost** — the filter runs on every admin page, so read from memory rather
  than calling a remote service.

Remove the filter when the plugin is deactivated, and its notices disappear.

## Sensitive Actions

A plugin route that must never run on a stolen session, such as publishing to
every site, adds `engine.RequireSecondFactor()` after `admin.RequirePermission`.
The signed-in user must have two-step verification on and send a fresh code
from their authenticator app with every request, in the `otp_code` form field
or the `X-GoPress-OTP` header:

```go
group.POST("/broadcasts/:id/publish",
    admin.RequirePermission(e.Auth, e.RBAC, "broadcast", "publish"),
    e.RequireSecondFactor(),
    h.publish)
```

Refusals are JSON `{"ok": false, "error": {"code": ...}}`:
`second_factor_not_enrolled` (403, two-step verification is off for the
account), `second_factor_invalid` (403, wrong or already used code),
`second_factor_locked` (429, too many wrong codes) or
`second_factor_unavailable` (503). Each code works once, and recovery codes are
not accepted here.

A handler that answers with a page rather than JSON calls
`engine.VerifySecondFactor(c)` instead: it checks the code the same way and
returns `""` or one of the codes above, so the handler can redirect back with
a message. `engine.HasSecondFactor(userID)` tells a page whether to show the
action as available.

On the page, add `data-second-factor` to the form: the admin asks for the code
in a dialog before sending the form and adds it as `otp_code`. A page can also
put its own `otp_code` field in the form. Show the action as unavailable, with a
link to `/admin/account/security`, while the account has two-step verification
off.

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

## Content-Security-Policy Sources

While a theme package is active, front-end responses carry a
Content-Security-Policy that runs only this site's scripts (see
[Theme Packages](../themes/theme-packages.md#content-security-policy)). A plugin
whose front-end output needs more, such as an inline payment widget loading a
provider's SDK, declares it with the `frontend.csp` filter:

```go
e.Hooks.AddFilter(hook.FrontendCSP, func(value interface{}, args ...interface{}) interface{} {
    sources, ok := value.(core.CSPSources)
    c, isRequest := args[0].(*gin.Context)
    if !ok || !isRequest || !strings.HasPrefix(c.Request.URL.Path, "/checkout/pay/") {
        return value
    }
    sources.Script = append(sources.Script, "https://js.provider.example")
    sources.Frame = append(sources.Frame, "https://pay.provider.example")
    sources.Connect = append(sources.Connect, "https://api.provider.example")
    return sources
}, 50)
```

- `core.CSPSources` has a list per directive: `Script`, `Style`, `Font`,
  `Image`, `Media`, `Frame`, `Connect`. Entries are hosts
  (`https://cdn.example.com`, `*.example.com`), `'self'`, and hashes of inline
  scripts (`'sha256-…'`); outside `Script`, also `https:`, `data:` and
  `blob:`.
- Sources that would let arbitrary scripts run are dropped and logged:
  `'unsafe-inline'`, `'unsafe-eval'`, `'unsafe-hashes'`, `'strict-dynamic'`,
  `*`, `https:` for scripts, and anything containing separators. Plain HTTP
  is accepted only for `localhost` and `127.0.0.1`.
- Return the same sources for the same path and settings: cached pages reuse
  their body and get the policy anew on each response.
- The filter is not called while a theme built into GoPress is active.
- A handler that sets its own `Content-Security-Policy` header keeps it.

## Translation Requirements

Admin-facing theme and plugin settings should not hard-code Chinese or English in templates. They should use the admin translation helper or component-owned locale files. If a component ships only one language, the admin should fall back to that available language instead of hiding the UI.

## Design Rule

Extensions should communicate with the admin through core hooks, providers, and template functions. Avoid direct imports between themes and plugins, and avoid post-processing full HTML responses to inject admin UI.

Every plugin must retain returned hook handles and remove them in
`Deactivate`. Core rebuilds the router and clears relevant cache paths after
activation changes so middleware, routes, admin UI, and frontend output reflect
the new state without a restart.
