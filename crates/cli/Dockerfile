# The CLI as a container: a Runner with no desktop app on the same computer.
#
#   docker build -f crates/cli/Dockerfile -t lorca-cli .
#   docker run -d -v lorca-data:/data -e LORCA_RELAY_URL=http://relay:18787 lorca-cli
#
# The local websocket binds 127.0.0.1 inside the container and accepts only a loopback Host
# (`crates/cli/src/ws.rs`). A published port arrives on the container's own address, not its
# loopback, so nothing outside reaches it: the bot side is driven through the relay. A container
# has no systemd user session, so `lorca service install` is refused; `--restart` keeps it running.

FROM rust:1.96-slim-trixie AS chef
RUN cargo install cargo-chef --version 0.1.78 --locked
WORKDIR /src

# The recipe is the workspace's manifests and lockfile, so the layer that compiles the
# dependencies stays cached until one of those changes.
FROM chef AS plan
COPY Cargo.toml Cargo.lock ./
COPY crates crates
RUN cargo chef prepare --recipe-path recipe.json

FROM chef AS build
COPY --from=plan /src/recipe.json recipe.json
RUN cargo chef cook --release --locked -p lorca --recipe-path recipe.json
COPY Cargo.toml Cargo.lock ./
COPY crates crates
RUN cargo build --release --locked -p lorca

# SQLite is bundled into the binary, and TLS roots are compiled in. The rest is what a bot's
# tools call: `git` and `bash` for the shell, `rg` for search, `wget` for the healthcheck. Node
# and `uv` start the marketplace's stdio plugins (`npx`, `uvx`); the rest of them are HTTP.
FROM debian:trixie-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates git bash ripgrep wget curl \
       nodejs npm python3 python3-venv \
    && rm -rf /var/lib/apt/lists/*
# `uvx` resolves to `uv tool run`, and pip's console scripts start with the venv's interpreter.
RUN python3 -m venv /opt/uv \
    && /opt/uv/bin/pip install --no-cache-dir uv \
    && ln -s /opt/uv/bin/uv /usr/local/bin/uv \
    && ln -s /opt/uv/bin/uvx /usr/local/bin/uvx
COPY --from=build /src/target/release/lorca /usr/local/bin/lorca
COPY --chmod=0755 crates/cli/docker-entrypoint.sh /usr/local/bin/lorca-entrypoint

ENV LORCA_HOME=/data
WORKDIR /data
EXPOSE 4862
# The entry point pairs or creates the identity on the first start, then runs the server.
ENTRYPOINT ["lorca-entrypoint"]
CMD ["serve"]
