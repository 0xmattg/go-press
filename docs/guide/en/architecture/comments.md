# Comments And Replies

Comments are a core CMS domain. They are stored separately from `Content`, so
they survive theme switches and never enter content archives, REST content
queries, taxonomy relationships, or multilingual content cloning.

## Data And Threading

`core/comment.Comment` belongs to one content row and one authenticated user.
Its optional `ParentID` points to another comment. The current policy supports
top-level comments plus one direct reply level; a reply to a reply is rejected
by the service. The schema can support a deeper policy later without a data
migration.

Statuses are `pending`, `approved`, `spam`, and `trash`. New comments default to
`pending`. A trusted server-side moderation policy may set `CreateInput.InitialStatus`
to `approved`; core rejects every initial value except `pending` and `approved`.
Never bind this field directly from browser input. Anonymous visitors see
approved comments only; an authenticated author also sees their own pending
comments.

## Enabling Comments For A Content Type

A content type opts in through the generic registry capability:

```go
Supports: []string{"title", "content", "comments"}
```

The content editor then exposes the per-item `CommentStatus` (`open` or
`closed`) control. `BaseTheme.renderSingle` injects:

| Template field | Meaning |
|---|---|
| `.Comments` | Approved comments plus the current user's pending comments |
| `.CommentCount` | Approved public count |
| `.CommentsOpen` | Registry support and per-content open state |
| `.CanComment` | Current user owns `comment.create` |

Themes own markup and interaction. They access comments through the optional
`theme.CommentApp` and authorization through `theme.PublicAuthorizationApp`;
they must not query the comment table or inspect a concrete identity provider.

## Authentication And RBAC

Writing requires a core public session and `comment.create`. Default subscriber,
contributor, author, and editor roles receive it. Moderation requires
`comment.moderate`, which is limited to editor and super admin by default.

A theme may declare an identity plugin in `theme.toml` when it requires that
assembly, but runtime code still uses only `currentUser`, `isLoggedIn`,
`loginURL`, and the generic authorization contract.

Every comment POST must enforce same-origin checks, validate the target content
and its registry capability, ensure a parent belongs to the same content row,
enforce reply depth, validate body length, and apply server-side rate limits.
Never authorize from hidden buttons alone.

## Moderation Policies And Owner Review

Core keeps the safe default of pending review while allowing a theme to apply a
server-owned setting for immediate approval. The theme must translate that
setting into `InitialStatus`; a form field or JSON property supplied by the
comment author is never authoritative.

`CommentService.ListVisibleForReview(contentID, viewerID)` returns the approved
thread together with its pending queue. The method deliberately does not decide
who may review the target. Before calling it—or changing any returned
comment—the route must prove one of these conditions:

- the account owns the target content and has the content type's `update_own`
  capability; or
- the account has global `comment.moderate` permission.

Every comment ID must also be checked against the expected content ID and
parent relationship. Owner moderation should be limited to replies on that
owner's content; global moderators retain spam, trash, takedown, and exceptional
review authority. Unauthorized and cross-content IDs must be rejected on the
server, regardless of which buttons the page renders.

## Profile Pages

Themes may render an own-account profile from `user.PublicUserView`. Use a fixed
route such as `/profile`, never a caller-supplied user ID, check
`profile.read_own`, and return `Cache-Control: private, no-store`. Do not expose
identity payloads, session tokens, password hashes, or another user's email.

## Cache And Hooks

Core emits `comment.created` and `comment.status_changed`. A transition into or
out of `approved` invalidates anonymous page cache entries, while pending-only
changes remain private and do not require a public flush.
