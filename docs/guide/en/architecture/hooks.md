# Hook System

GoPress hooks are the Go equivalent of WordPress `do_action` and
`apply_filters`. They are the primary way plugins and themes extend core without
importing one another.

## API

```go
// Action: side effects only.
e.Hooks.AddAction(name, fn, priority) hook.Handle
e.Hooks.RemoveAction(handle)
e.Hooks.DoAction(ctx, name, args...)

// Filter: transform one value through an ordered chain.
e.Hooks.AddFilter(name, fn, priority) hook.Handle
e.Hooks.RemoveFilter(handle)
e.Hooks.ApplyFilter(name, value, args...) interface{}
```

Lower priority numbers run first. Every registration returns a `hook.Handle`;
plugins must retain those handles and remove them during `Deactivate` so runtime
disable is complete.

Hook names and payloads are public contracts. Keep payloads stable and generic;
do not expose a theme-private struct or provider SDK type to the other side. A
filter with no subscribers must return its input unchanged, and an action with
no subscribers must have no effect.

## Engine Lifecycle Hooks

| Hook | Timing | Arguments |
|---|---|---|
| `engine.init` | After bootstrap completes. | `*core.Engine` |
| `middleware.early` | Before page-cache middleware. | `*gin.Engine` |
| `routes.register` | After admin routes and before the frontend catch-all. | `*gin.Engine` |
| `options.bulk_updated` | After admin saves options in bulk. | None |

## Theme Template Slots

Plugins contribute local frontend HTML through semantic slots declared by the
theme:

| Hook | Go constant | Location and typical use |
|---|---|---|
| `header.nav.after` | `hook.ThemeHeaderNavAfter` | End of the primary navigation list; language switchers or account menus. |
| `theme.head.end` | `hook.ThemeHeadEnd` | Before `</head>`; verification meta, analytics, preconnect, or CSS. |
| `theme.body.open` | `hook.ThemeBodyOpen` | Immediately after `<body>`; noscript tags, bootstrap code, or banners. |
| `theme.footer.end` | `hook.ThemeFooterEnd` | Before `</body>` after theme scripts; deferred scripts or widgets. |

```gotemplate
<ul class="nav-menu">
  {{$homeURL := langPrefixURL .Ctx "/"}}
  <li><a href="{{$homeURL}}" class="{{if isMenuURLActive .Ctx $homeURL}}active{{end}}">{{T .Ctx "nav_home"}}</a></li>
  {{renderHook "header.nav.after" .}}
</ul>
```

Global slots belong in the base layout and should appear exactly once:

```gotemplate
<head>
  ...
  {{renderHook "theme.head.end" .}}
</head>
<body>
  {{renderHook "theme.body.open" .}}
  ...
  {{renderHook "theme.footer.end" .}}
</body>
```

See [Theme Frontend Slots](../themes/overview.md#frontend-extension-slots).

## Menu Hooks

| Hook | Go constant | Contract |
|---|---|---|
| `menu.location.resolve` | `hook.MenuLocationResolve` | Filter after location lookup. Value: `*menu.Menu`; args: `location string`. |
| `menu.deleted` | `hook.MenuDeleted` | Action after deletion. Args: `menuID uint`. |

Language-aware menu assignment is implemented through these generic hooks;
`core/menu` has no multilingual-specific branch.

## Admin Extension Hooks

| Hook | Go constant | Contract |
|---|---|---|
| `admin.content_list.tabs` | `admin.HookContentListTabs` | Filter list tabs. Value: `[]admin.ContentListTab`; args: `*gin.Context, typeName string`. |
| `admin.content.permalink_prefix` | `admin.HookContentPermalinkPrefix` | Filter an editor permalink prefix. Value: `string`; args: `*gin.Context, *content.Content`. |
| `admin.content_form.fields` | `hook.AdminContentFormFields` | Filter editor meta-box HTML. Value: `template.HTML`; args: `*content.Content, *content.ContentTypeDef`. |
| `admin.content.saved` | `hook.AdminContentSaved` | Action after the content row is saved. Args: `*gin.Context, *content.Content`. |

See [Admin Extension Points](../admin/extension-points.md).

## SEO Hook

`seo.content.meta` (`hook.SEOContentMeta`) filters a single content page's
`rewrite.SEOMeta` before rendering. Its arguments are the content row and its
metadata map. The [SEO Extras plugin](../plugins/seo-extras.md) is the reference
implementation.

## Sitemap Transformers

```go
type TransformerHandle
handle := e.Sitemap.AddTransformer(fn)
e.Sitemap.RemoveTransformer(handle)
```

A transformer can add alternate-language links or derived URL entries. The
remove handle provides the same hot-disable guarantee as actions and filters.

## Extension Pattern

1. Core defines the data contract, hook name, and trigger point.
2. A plugin registers actions or filters during activation and saves handles.
3. With no plugin, filters pass values through and actions have no side effects.
4. Deactivation removes every handle and any symmetric transformer registration.

This pattern keeps core complete on its own while allowing features to be
composed without theme/plugin cross-dependencies.
