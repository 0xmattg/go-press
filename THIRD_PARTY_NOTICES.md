# Third-Party Notices

GoPress is licensed under the MIT License (see [LICENSE](LICENSE)). The
third-party material below is distributed with this repository under its own
license and is **not** covered by the GoPress MIT License.

## DB-IP Lite Country database

- Files:
  - `plugins/gopress-analytics/data/dbip-country-lite.csv.gz`
  - `plugins/gopress-update-registry/data/dbip-country-lite.csv.gz`
- Source: [DB-IP](https://db-ip.com) — IP to Country Lite
- License: [Creative Commons Attribution 4.0 International (CC BY 4.0)](https://creativecommons.org/licenses/by/4.0/)
- Attribution: [IP Geolocation by DB-IP](https://db-ip.com)

Sites that display data derived from this database must keep the attribution
above. The analytics plugin can download a newer copy at runtime; the
downloaded file remains under the same license.

## Go module dependencies

Go modules listed in `go.mod` are fetched at build time and keep their own
licenses. Binary distributions of GoPress should include the license notices of
those modules.
