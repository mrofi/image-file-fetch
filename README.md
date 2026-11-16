# image-file-fetch

A small Go webserver that extracts one exact file, or one directory as a
tar, from a container image and streams it back over HTTP — no Docker daemon, no image
listing, no disk writes. It pulls the image directly from its
registry, flattens its layers in a stream, and copies the matching
file straight into the HTTP response. Symlinks (e.g. `/etc/os-release`
in most base images) are followed transparently.

## Usage

Run the server:

    go run .
    # or: ADDR=:9090 go run .

Download a file:

    curl -X POST http://localhost:8080/download \
      -H 'Content-Type: application/json' \
      -d '{"image": "alpine:latest", "path": "/etc/os-release"}' \
      -o os-release

### A whole directory

Add `"archive": "tar"` and `path` is a directory: everything under it
comes back as a tar stream (`application/x-tar`, no `Content-Length`),
with entry names relative to that directory.

    curl -X POST http://localhost:8080/download \
      -H 'Content-Type: application/json' \
      -d '{"image": "nginx:alpine", "path": "/usr/share/nginx/html", "archive": "tar"}' \
      | tar -x -C ./html

- Regular files, directories and symlinks are included. Devices and other
  special files are left out.
- An absolute symlink that points inside the directory is rewritten to a
  relative one, so it still works after unpacking elsewhere. Other
  symlinks are passed through as they are.
- A hard link is kept only when the file it names is inside the directory.
- If `path` is itself a symlink to a directory, it is followed.
- `404` when the directory is not in the image, `400` when `path` is a
  file (leave out `archive` to fetch a file).
- If the image turns out to be unreadable after the response has started,
  the stream is cut short: check that the tar unpacks without error.

### Private images

Pass registry credentials in the request body — they are used only
for that single request, never stored:

    curl -X POST http://localhost:8080/download \
      -H 'Content-Type: application/json' \
      -d '{"image": "registry.example.com/repo:tag", "path": "/app/config.yaml", "username": "user", "password": "token"}' \
      -o config.yaml

## CLI mode

Instead of running the server, fetch a single file directly from the
command line:

    go run . fetch alpine:latest /etc/os-release
    # writes ./os-release

    go run . fetch -output - alpine:latest /etc/os-release
    # streams to stdout

    go run . fetch -archive nginx:alpine /usr/share/nginx/html
    # writes ./html.tar

    go run . fetch -archive -output - nginx:alpine /usr/share/nginx/html | tar -x -C ./html

Flags: `-archive` to fetch a directory as a tar,
`-username` / `-password` for private images, `-output FILE`
(defaults to the basename of `<path>` in the current directory; use
`-output -` for stdout).

## Running as a container

    docker build -t image-file-fetch .
    docker run --rm -p 8080:8080 image-file-fetch

## Security

Registry credentials travel in the request body — always run this
behind TLS in any real deployment, never expose it over plain HTTP
outside local development.

## Design

See [docs/superpowers/specs/2026-08-29-image-file-fetch-design.md](docs/superpowers/specs/2026-08-29-image-file-fetch-design.md).

## License

MIT
