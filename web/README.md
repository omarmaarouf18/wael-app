# wael-app/web (mirror)

**Mirror of `wael-app/web`, do not commit here.**

This repository is a one-way mirror of the `web/` folder of
`omarmaarouf18/wael-app` (the source of truth). Every commit here is produced
by the "Sync Website" workflow in `wael-app`
(`git subtree split --prefix=web`), and the next sync overwrites it.

- Do not commit, push, fork or open pull requests against this repository.
- To change the website, change `web/` in `wael-app` and let the sync push it.
- The decision record is ADR-0014 (`docs/adr/0014-sales-website.md` in
  `wael-app`); the website's own documentation is `docs/web/README.md` in
  `wael-app`.

The website is deployed by Vercel from this repository, with the Root
Directory `/` (the mirror's root is the website's root).
