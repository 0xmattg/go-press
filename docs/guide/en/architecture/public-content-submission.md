# Public Content Submission

GoPress provides a generic core service for authenticated users to create and
maintain content through theme-owned frontend workflows. It is intended for
questions, community topics, listings, and similar user-authored content
without making any of those business concepts part of core.

## Architecture Boundary

The responsibility split is deliberate:

- **Core** owns the content model, policy evaluation, active-account checks,
  RBAC, ownership enforcement, status assignment, input limits, slug
  uniqueness, sanitization, rate limiting, and persistence.
- **Themes** own routes, forms, templates, content-type-specific validation,
  and the decision whether a trusted site policy permits immediate publishing.
- **Identity plugins** only authenticate users. A submission workflow must not
  import or inspect a concrete identity provider.

Core does not create frontend routes or UI when a policy is enabled. A theme
must opt in declaratively and then consume the generic service.

## Declaring A Policy

Add a nested policy to a theme-defined content type in `theme.toml`:

```toml
[[content_types]]
name = "question"
label = "Question"
label_plural = "Questions"
supports = ["title", "content", "excerpt", "comments"]
has_archive = true
rewrite_slug = "questions"

[content_types.public_submission]
enabled = true
roles = ["subscriber", "contributor"]
default_status = "pending"
allow_update_own = true
allow_delete_own = true
```

The role list is deny-by-default: an empty list allows nobody. `default_status`
accepts `draft`; every other value safely falls back to `pending`. Enabling the
policy does not automatically grant global edit or moderation access.

While the theme is active, core converts the declaration into temporary
content-type capabilities:

- `<type>.create`
- `<type>.read_own`
- `<type>.update_own` when `allow_update_own` is enabled
- `<type>.delete_own` when `allow_delete_own` is enabled

Theme-created grants are revoked when the theme changes. Capabilities that
already existed before activation are preserved.

## Theme Integration

Access the service through the optional `theme.PublicSubmissionApp` contract:

```go
host, ok := app.(theme.PublicSubmissionApp)
if !ok || host.PublicSubmissionService() == nil {
    return
}
submissions := host.PublicSubmissionService()
```

Create, update, and trash operations use the authenticated account ID rather
than trusting an author field submitted by the browser:

```go
item, err := submissions.CreateOwn(c, content.PublicSubmissionInput{
    ContentType: "question",
    UserID:      account.ID,
    Title:       c.PostForm("title"),
    Content:     c.PostForm("content"),
    Excerpt:     c.PostForm("excerpt"),
})
```

The available operations are `CreateOwn`, `UpdateOwn`, and `TrashOwn`.
Theme-owned activity/list pages should separately require `<type>.read_own` and
scope every query to the authenticated author's ID.

## Editorial Status

Public submissions normally enter `pending` or `draft` according to the
declared policy. `PublishImmediately` is a trusted server-side policy input:
when true, core stores `published` and sets `PublishedAt`.

Never bind `PublishImmediately` directly from form or JSON input. A theme may
set it only after applying a server-owned moderation setting or another trusted
authorization rule. A user-controlled checkbox must not bypass review.

The shared admin editor recognizes `published`, `pending`, `draft`, and
`archived`. It ignores forged status values and does not allow the normal editor
to move content directly to `trash`.

## Validation And Abuse Limits

Core enforces the following rules even if browser validation is bypassed:

- the current public session must match `UserID`, and the database account must
  still be active;
- the content type must have an enabled policy, explicitly allow the account
  role, and grant the requested RBAC action;
- update and delete require matching content type and ownership; a foreign ID
  is returned as not found to reduce resource enumeration;
- title and body are required; limits are 240 title runes, 100,000 body runes,
  and 1,000 excerpt runes;
- slugs are normalized to Unicode letters and numbers, limited to 120
  characters, and made globally unique without inheriting request content
  scopes;
- creates are limited to 3 per minute and 20 per rolling 24 hours for each
  user/content-type pair; trashed rows still count;
- content is persisted through the normal sanitized content repository path.

Themes may add stricter validation but must not weaken these rules.

## Route Security Checklist

Every theme-owned submission route must:

1. require a valid public session and a concrete `<type>.<action>` capability;
2. derive the acting user from the request context rather than a URL, form, or
   JSON user ID;
3. enforce same-origin checks on every state-changing request;
4. call the core service for writes instead of saving content directly;
5. verify type and ownership for any resource ID and avoid revealing foreign
   resources;
6. return `Cache-Control: private, no-store` for account activity, drafts, and
   moderation queues;
7. include tests proving anonymous and permissionless users are rejected and
   owners are accepted.

If the content type also supports comments, keep comment creation and
moderation on the independent contracts documented in
[Comments and Replies](comments.md).
