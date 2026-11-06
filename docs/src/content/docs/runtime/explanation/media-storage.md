---
title: Media Storage
description: How the runtime's local file store deduplicates media by content hash and writes files atomically.
sidebar:
  order: 5
verified:
  commit: 004b9cbbd4d40f770a2e3c76053b600a55189355
  sources:
    - runtime/storage/local/filestore.go
---
`runtime/storage` defines the interfaces for storing the media content (images, audio, video) that conversations reference. `runtime/storage/local` provides `FileStore`, a filesystem-backed implementation. This page explains two behaviors of `FileStore` that the API surface does not show: content deduplication and atomic writes.

## Content-addressed deduplication

Set `FileStoreConfig.EnableDeduplication` to turn on deduplication. The store then works as follows:

1. It hashes the content with SHA-256 on store.
2. It looks the hash up in an in-memory index, persisted as `.dedup_index.json` in the store's base directory and loaded when the store opens.
3. If the hash exists and its file is still on disk, the store returns the existing file's reference and increments its reference count. It writes no new copy. If a retention policy or anything else has deleted the file, the store drops the index entry and treats the hash as new.
   - When the policy handler reports expiry times, as `policy.TimeBasedPolicyHandler` does, a hit whose retention outlasts the shared file's extends it. Only the policy and the expiry in the file's metadata change: the expiry is recorded as `expires_at`, and the run, session, timestamp and other fields stay those of the store that wrote the file. The file then lasts as long as the longest-lived store that references it. A hit that names no policy, in a store with no `DefaultPolicy`, leaves the file's retention as it is.
4. If the hash is new, the store writes the content and adds an index entry with a count of one. Content stored again after its file was deleted gets a fresh path. References handed out before the deletion stay dead, and deleting through one leaves the new copy in place.
5. `DeleteMedia` decrements the count while it is above one. The file and its index entry are removed when the last reference is deleted.

Repeated media such as profile pictures, template assets and regenerated images cost one copy on disk however many times you reference them.

The trade-offs:

- Every store pays a hashing cost.
- Reference counts live in memory. The index file records hash-to-path entries only, so a restarted store counts each existing file as one reference.
- A mutex guards the index within one process. Two processes sharing a base directory are not coordinated.
- The index is written when it changes; call `Flush` before shutdown to persist pending changes.

## Atomic writes

`FileStore` writes content to a temporary file first, then renames it into place. A rename is atomic on POSIX filesystems, so readers never observe a partially written file. A temporary file left behind by a crash is abandoned data and is never read as media.

## Related

- [Storage reference](/runtime/reference/storage/): the `storage` package API
- [State Management](/runtime/explanation/state-management/): the conversation state store, a separate concern from media storage
