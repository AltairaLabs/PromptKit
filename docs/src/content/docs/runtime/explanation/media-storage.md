---
title: Media Storage
sidebar:
  order: 5
---
Understanding how Runtime stores and deduplicates media content.

## Overview

`runtime/storage` defines the interfaces used to store media content (images, audio, video)
referenced from conversations. `runtime/storage/local` provides a filesystem-backed
implementation, `FileStore` — this page explains two of its behaviors that aren't obvious from the
API surface alone: content deduplication and crash-safe writes.

## Content-Addressed Deduplication

`FileStore` supports opt-in deduplication using SHA-256 content hashing:

1. **Hash**: content is hashed with SHA-256 on store.
2. **Look up**: the hash is checked against a `.dedup_index.json` index file in the store's base
   directory.
3. **Reuse**: if the hash already exists, the existing file is reused and its reference count is
   incremented — no new copy is written.
4. **New content**: if the hash is new, the content is stored and a new index entry is created.
5. **Delete**: deleting a reference decrements the count; the underlying file is only removed once
   the count reaches zero.

This makes repeated media (profile pictures, template assets, frequently regenerated images) cheap
to reference many times without duplicating bytes on disk. The trade-off is a per-store hashing
cost and a shared index file that requires locking across concurrent writers.

## Atomic Writes

`FileStore` writes are crash-safe: content is written to a temporary file in the target directory
first, then renamed into place. The atomic rename means readers never observe a partially-written
file, and any leftover temp file after a crash is just abandoned data, not corruption.

## See Also

- [Storage Reference](/runtime/reference/storage/) — complete `storage` package API
- [State Management](/runtime/explanation/state-management/) — the conversation state store, a
  separate concern from media storage
