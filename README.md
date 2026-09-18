# prodeko.org

A git-based website for Prodeko. Pages are Markdown files, lists of people are
YAML files, and Hugo turns them into plain HTML. Editors sign in with their
normal Prodeko account and never see git.

See [the design document](docs/superpowers/specs/2026-09-18-prodeko-git-cms-design.md)
for how it works and [the roadmap](docs/roadmap.md) for what is being built.

## Layout

```
site/     the Hugo site, the templates, and the Decap configuration
proxy/    the service that lets editors sign in with a Prodeko account
tools/    one-off scripts, such as reading the old Django database
docs/     design, roadmap and the written deliverables
```

Deployment lives in the separate infra-prodeko repository: an Ansible role for
Caddy and the proxy container. The built site is copied to the server over SSH
and served from disk.

## Running the site locally

Hugo extended, version 0.166.0 or newer:

```
cd site
hugo server
```

The site is bilingual, with Finnish under `content/fi/` and English under
`content/en/`. Two pages are translations of each other when they share a
`translationKey` in their front matter, which is what lets the Finnish and
English addresses differ.
