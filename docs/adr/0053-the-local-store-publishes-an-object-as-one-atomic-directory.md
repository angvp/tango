# The local store publishes an object as one atomic directory

`storage/local` must make the promise every `Store` makes: an object is fully visible or absent. A file holding the bytes next to a file holding the metadata cannot keep it, because renaming them one after the other leaves a window where one exists without the other, and a crash in that window leaves a partial object that `Stat` or `Serve` could report.

An object is therefore a directory holding `data` and `meta` (size, SHA-256, content type, creation time). `Put` writes both into a temporary directory under the root, syncs them, and renames that directory to the object's final path, fanned out by the first two characters of the key. Creating the final path is one rename, so the bytes and the metadata appear together or not at all, and renaming onto an existing non-empty object fails, which makes the write create-only. `Stat`, `Open` and `Serve` treat an object as present only when its directory holds a readable `meta` and a `data` file of the size `meta` records; an empty directory, a directory missing a file, corrupt metadata or truncated data all read as not found. `Delete` renames the directory out of place and then removes it, so a delete is also never half-visible. Everything is reached through `os.Root`, so no key and no symlink inside the root can name a path outside it; directories are `0700` and files `0600`. Temporary directories live apart from published objects, are never read, and are swept on open once they are an hour old, so opening a second store on the same root does not remove another writer's work in progress.

Rejected:

- **Two files renamed independently.** A crash between the renames shows a partial object.
- **A database or sidecar index.** Another place for the bytes and their description to disagree.
- **Writing in place and renaming the data file only.** The metadata would still be a separate step.
- **Hand-written path-prefix checks.** `os.Root` gives the containment guarantee, symlinks included, from the standard library.

Consequences: the on-disk layout is an implementation detail and not covered; the backend is for one host, because a rename is atomic only on one filesystem; and a leftover temporary directory costs disk until the next open, never visibility.
