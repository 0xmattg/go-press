# Admin Extension Points

Admin extension points let plugins and themes add behavior to the CMS while keeping the core admin stable.

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

## Translation Requirements

Admin-facing theme and plugin settings should not hard-code Chinese or English in templates. They should use the admin translation helper or component-owned locale files. If a component ships only one language, the admin should fall back to that available language instead of hiding the UI.

## Design Rule

Extensions should communicate with the admin through core hooks, providers, and template functions. Avoid direct imports between themes and plugins, and avoid post-processing full HTML responses to inject admin UI.
