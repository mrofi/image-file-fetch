# image-file-fetch

A small Go webserver that extracts one exact file from a container
image and streams it back over HTTP — no Docker daemon, no image
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

### Private images

Pass registry credentials in the request body — they are used only
for that single request, never stored:

    curl -X POST http://localhost:8080/download \
      -H 'Content-Type: application/json' \
      -d '{"image": "registry.example.com/repo:tag", "path": "/app/config.yaml", "username": "user", "password": "token"}' \
      -o config.yaml

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
