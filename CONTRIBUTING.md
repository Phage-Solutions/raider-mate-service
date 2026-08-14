# Contributing

Thanks for looking at Raider Mate. A few things before you send a patch.

## Licensing

This project is AGPLv3 (see `LICENSE`). Contributions are accepted under the same
license. There is no CLA; sign-off (below) is what we use instead.

## Developer Certificate of Origin

Every commit must be signed off, certifying you wrote it or otherwise have the right to
submit it under the project's license:

```
git commit -s
```

That appends a line like:

```
Signed-off-by: Jane Doe <jane@example.com>
```

Pull requests with unsigned commits will be asked to rebase and re-sign before review.

The full text of the certification:

```
Developer Certificate of Origin
Version 1.1

Copyright (C) 2004, 2006 The Linux Foundation and its contributors.

Everyone is permitted to copy and distribute verbatim copies of this
license document, but changing it is not allowed.

Developer's Certificate of Origin 1.1

By making a contribution to this project, I certify that:

(a) The contribution was created in whole or in part by me and I
    have the right to submit it under the open source license
    indicated in the file; or

(b) The contribution is based upon previous work that, to the best
    of my knowledge, is covered under an appropriate open source
    license and I have the right under that license to submit that
    work with modifications, whether created in whole or in part
    by me, under the same open source license (unless I am
    permitted to submit under a different license), as indicated
    in the file; or

(c) The contribution was provided directly to me by some other
    person who certified (a), (b) or (c) and I have not modified
    it.

(d) I understand and agree that this project and the contribution
    are public and that a record of the contribution (including all
    personal information I submit with it, including my sign-off) is
    maintained indefinitely and may be redistributed consistent with
    this project or the open source license(s) involved.
```

## Working in this repo

Read `AGENTS.md` first. It has the hard rules (tier gating, role ownership, no
`discordgo` types here, HATEOAS link generation) and the conventions the codebase
follows. `docs/design.md` has the schema and algorithms; `docs/style.md` has the writing
style for anything a human reads, including PR descriptions.

Before opening a PR:

```
make lint
make test
```

Small commits, one concern each.
