# Media Storage Reference Example

Send media to a vision model by a **durable storage reference** instead of
inlining bytes on every request.

The caller stores an image once in a `MediaStorageService`, then passes a
durable reference on each request. At model-call time the provider resolves
that reference through the store — to a model-fetchable URL (via the store's
`GetURL`) or to bytes. With a cloud-backed store the model fetches the
presigned URL directly, so **no image bytes transit the app** on each turn.

## What it shows

- Creating a local disk-backed store with `local.NewFileStore`
- Storing an image once with `StoreMedia` and getting back a `Reference`
- Wiring the store into a conversation with `sdk.WithMediaStorage`
- Sending the image by reference with `sdk.WithImageStorageRef`

## Run it

```bash
export OPENAI_API_KEY=your-key   # or GEMINI_API_KEY / ANTHROPIC_API_KEY
go run .
```

The example writes the stored image under `./data`. It generates a tiny PNG
in code so it is fully self-contained — the point is the reference wiring, not
the image content.

## Test it (no keys needed)

```bash
go test ./ -count=1
```

The smoke test uses the mock provider and a real local store, asserting that
`Open` + `Send` succeed with an image sent by reference.
