# Upgrade fixtures

A database from every Vaquero release. `TestFixturesMigrateToHead` (internal/db) migrates each one to the newest schema and checks that nothing was lost: every row is still there with every value it had, foreign keys hold, and search still finds what it found. This is how "an upgrade never loses data or needs manual steps" is kept.

Before each release:

    make fixture VERSION=0.2.0
    git add testdata/upgrade && git commit

The release workflow refuses to run without the fixture for the tag it releases, or when the fixture's schema isn't the one that commit migrates to.

Never edit, regenerate or delete a released version's fixture: each one stands for databases people have. A fixture for a version not yet released is different: if a migration lands before the release, delete it and run `make fixture` again, so it matches what ships. When a later migration adds rows to a table on purpose (a seeded setting, say), add the table to `grownTables` in the test rather than changing the fixture.
