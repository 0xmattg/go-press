# GoPress Analytics

`gopress-analytics` is GoPress's first-party, self-hosted traffic analytics
plugin. It adds PV, UV, new-visitor, session, daily-trend, top-page, visitor
mix, and IP country distribution metrics to the admin dashboard without
requiring a third-party analytics service.

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
metrics, exact visitor/day bridge rows, daily-dimension aggregate rows, and
daily dimension/visitor bridge rows used for exact dimension UV counts. Raw
events include:

- Path, language, referrer and UTM attribution.
- Plain IP address plus a keyed irreversible IP hash.
- User-Agent and normalized device type, platform, vendor, browser, and OS.
- Country code resolved from a local DB-IP Lite GeoIP database when available.

Plain IP and User-Agent values are raw personal data. The retention setting
removes expired events and sessions after 30, 60, 90, or 180 days. Aggregate
daily metrics and pseudonymous visitor/day bridge rows remain so historical
7/30/90-day reporting stays accurate. Update the site's privacy policy and
cookie notice before enabling collection where required.

## GeoIP Country Database

The plugin can keep a local DB-IP Lite country database at
`plugins/gopress-analytics/data/dbip-country-lite.csv.gz`. The settings page
shows whether the file is loaded, the record count, and the file timestamp.
Users with `plugin.update` can click the GeoIP update button to download the
current monthly database into the plugin directory.

GeoIP lookup is performed against an in-memory index after the file is loaded,
so page requests do not call external services. Country metrics are written to
the generic `daily_dimensions` tables with exact UV de-duplication through the
dimension visitor bridge table. If the file is missing, invalid, or the visitor
IP is private/local, country remains empty and collection continues normally.
When using the default DB-IP Lite source, follow its license, attribution, and
privacy requirements for the site deployment.

## Permissions

The dashboard JSON endpoint and raw event query endpoint require
`analytics.read`. Super administrators inherit it through the wildcard policy;
editors receive a runtime read grant. Other roles do not receive analytics
data. Plugin settings remain protected by the core `plugin.read` and
`plugin.update` permissions, and the GeoIP file update route specifically
requires `plugin.update`.

## Current MVP Boundary

The first version provides aggregate traffic reporting and country distribution.
Engagement time, conversions, device distribution, realtime views, funnels,
export, and broader raw-event investigation UI are reserved for later phases.
