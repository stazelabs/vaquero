# Upgrade fixtures

A database from every Vaquero release. `TestFixturesMigrateToHead` (internal/db) migrates each one to the newest schema and checks that nothing was lost: every row is still there, foreign keys hold, and search still finds what it found. This is how "an upgrade never loses data or needs manual steps" is kept.

Before each release:

    make fixture VERSION=0.2.0
    git add testdata/upgrade && git commit

The release workflow refuses to run without the fixture for the tag it releases.

Never edit, regenerate or delete a fixture: each one stands for databases people have. When a later migration adds rows to a table on purpose (a seeded setting, say), add the table to `grownTables` in the test rather than changing the fixture.
