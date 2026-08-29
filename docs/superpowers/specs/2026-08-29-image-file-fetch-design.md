# image-file-fetch — design

## Purpose

A small Go webserver that, given a container image reference and an exact
path inside that image, streams the contents of that one file back over
HTTP — without needing a Docker daemon, without listing the image's file
tree, and without writing anything to local disk.

## Non-goals

- Browsing or listing an image's file tree.
- Downloading more than one file per request, or whole directories.
- Caching pulled images or file contents across requests.
- Running behind a Docker daemon (`docker.sock`) or requiring privileged
  container access.
- Multi-replica/session-affinity support (deployed as a single instance;
  see Deployment below).

## Architecture

A single Go binary using the standard library `net/http` and
[`google/go-containerregistry`](https://github.com/google/go-containerregistry)
(`name`, `remote`, `mutate` packages) to pull images directly from a
registry over HTTPS.

There is no persistent state and no local disk usage: each request pulls
the image, flattens its layers in a stream, scans that stream for the
requested path, and pipes the matching entry's bytes straight into the
HTTP response.

## API

### `POST /download`

Request body (JSON):

```json
{
  "image": "registry.example.com/repo:tag",
  "path": "etc/nginx/nginx.conf",
  "username": "optional",
  "password": "optional"
}
```

- `image` (required) — a reference parseable by
  `name.ParseReference` (registry/repo:tag or @digest; defaults to
  Docker Hub if no registry host is given, matching normal `docker pull`
  semantics).
- `path` (required) — the exact path to the file inside the image's
  merged filesystem. No listing/searching is performed; an incorrect
  path returns 404.
- `username` / `password` (optional) — registry credentials for private
  images. Used only to build the auth for this single request
  (`authn.Basic`); never persisted, logged, or reused across requests.
  Omitted entirely → `authn.Anonymous`.

Response: on success, the raw file bytes with
`Content-Disposition: attachment; filename="<basename of path>"` and a
best-effort `Content-Type` guessed from the file extension
(`mime.TypeByExtension`, falling back to `application/octet-stream`).

### Request flow

1. Validate the request body: `image` and `path` both required and
   non-empty → 400 otherwise.
2. Build the `authn.Authenticator`: `authn.Basic{Username, Password}` if
   both credential fields are non-empty, else `authn.Anonymous`.
3. `name.ParseReference(image)` → parse failure returns 400.
4. `remote.Image(ref, remote.WithAuth(auth))` to fetch the manifest and
   set up lazy layer access. Auth/network/not-found failures from the
   registry are passed through as the closest matching HTTP status
   (401/403 for auth, 404 for missing image/tag, 502 for other registry
   errors).
5. `mutate.Extract(img)` → a single `io.ReadCloser` tar stream of the
   flattened filesystem (whiteouts already resolved by the library —
   the caller only ever sees the final merged view).
6. `tar.NewReader` over that stream; iterate entries in order. Normalize
   both the requested `path` and each `header.Name` (strip any leading
   `./` or `/`, `path.Clean`) before comparing, so `/etc/foo` and
   `etc/foo` are treated the same.
   - Non-matching entries: do not read their content into memory —
     advance past them (the tar reader supports this without an
     explicit skip call; simply not reading `Next()`'s returned bytes
     and calling `Next()` again discards them).
   - Matching entry that is a regular file (`Typeflag == tar.TypeReg`):
     write response headers, then `io.Copy(w, tarReader)` and return.
   - Matching entry that is not a regular file (directory, symlink,
     etc.): treat as not found — return 404 (a bare "download this path"
     API has no sensible behavior for a directory).
7. Reaching end-of-stream with no match → 404 `file not found in image`.

Nothing is ever written to a temp file or directory, so there is no
cleanup step and no risk of orphaned data if a request is interrupted
partway through.

## Error handling summary

| Condition                                   | Status |
|----------------------------------------------|--------|
| Missing/empty `image` or `path`               | 400    |
| Malformed image reference                     | 400    |
| Registry auth rejected                        | 401/403|
| Image or tag not found                        | 404    |
| Requested path not found in image, or not a regular file | 404 |
| Other registry/network error                  | 502    |

## Deployment

Runs as a plain, unprivileged container — no `docker.sock`, no host
mounts, no privileged mode — since all image access goes over the
registry's HTTPS API. This makes it a natural fit for any cloud
container service (Cloud Run, Fargate, GKE, DigitalOcean App Platform,
etc.).

Two things to account for at deploy time:

- **No session state to worry about**: since every request is fully
  self-contained (no temp files, no in-memory session map), there is no
  session-affinity requirement even under autoscaling. (Deployed as a
  single instance for now per current requirements, but the design does
  not itself impose that constraint.)
- **TLS required**: registry credentials travel in the request body, so
  this must run behind TLS (either terminated by the platform/ingress,
  or served directly) in any real deployment — never expose this
  endpoint over plain HTTP outside of local development.

## Testing

- Unit test: path normalization/matching (leading slash, `./` prefix,
  `path.Clean` edge cases).
- Unit test: non-regular-file match (directory/symlink) is treated as
  404.
- Integration test: pull a small public image (e.g. `alpine`) and
  download a known file end-to-end against the real registry — mocking
  the registry protocol adds little value here since
  go-containerregistry's own test suite already covers protocol
  correctness.
- Integration test: request a path that doesn't exist in the image →
  404.

## Repo

- New repo `image-file-fetch`, hosted on GitHub under `mrofi` (matching
  the existing `baseimage` repo's pattern: README, LICENSE, `.github`
  workflow).
