# GoPress Analytics

`gopress-analytics` is GoPress's first-party, self-hosted traffic analytics
plugin. It adds PV, UV, new-visitor, session, daily-trend, and top-page metrics
to the admin dashboard without requiring a third-party analytics service.

## Collection

The plugin registers middleware through `middleware.early`, before page cache,
so cached page views are counted. It collects successful frontend `GET` HTML
responses and excludes admin, API, static, health, Swagger, sitemap, robots,
favicon, and identified bot requests.

Request handling only publishes an event to a bounded in-memory queue. The
plugin worker uses a five-second window and aggregates visitors, sessions,
days, and pages in memory before writing batches to PostgreSQL, so analytics
storage is not on the page rendering critical path. A window retains at most
2,000 raw events by default. Additional events are dropped and counted
internally, preventing abnormal traffic from causing unbounded memory growth
or continuously amplifying database writes.

## Data and Privacy

Plugin-owned tables store events, visitors, sessions, daily metrics, page
metrics, exact visitor/day bridge rows, and a reserved daily-dimension aggregate
table for later source/device/geography reporting. Raw events include:

- Path, language, referrer and UTM attribution.
- Plain IP address plus a keyed irreversible IP hash.
- User-Agent and normalized device type, platform, vendor, browser, and OS.
- Reserved country and region fields for a future GeoIP integration.

Plain IP and User-Agent values are raw personal data. The retention setting
removes expired events and sessions after 30, 60, 90, or 180 days. Aggregate
daily metrics and pseudonymous visitor/day bridge rows remain so historical
7/30/90-day reporting stays accurate. Update the site's privacy policy and
cookie notice before enabling collection where required.

## Permissions

The dashboard JSON endpoint requires `analytics.read`. Super administrators
inherit it through the wildcard policy; editors receive a runtime read grant.
Other roles do not receive analytics data. Plugin settings remain protected by
the core `plugin.read` and `plugin.update` permissions.

## Current MVP Boundary

The first version provides aggregate traffic reporting. Engagement time,
conversions, geography, device distribution, realtime views, funnels, export,
and raw-event investigation UI are reserved for later phases.
