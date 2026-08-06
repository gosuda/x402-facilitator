FROM golang:1.24.3 AS builder

WORKDIR /app

COPY go.mod go.sum ./

RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o facilitator ./cmd/facilitator

FROM debian:stable-slim

RUN apt update -y && apt install -y ca-certificates \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY --from=builder /app/facilitator /app
# Without this the image builds and cannot run: `docker run <image> --config …` tries to exec the
# flag, because nothing here says what the container's process is.
#
# The config path is a default rather than a hard-coded argument, so mounting a file elsewhere and
# passing --config still works.
ENTRYPOINT ["/app/facilitator"]
CMD ["--config", "/app/config.toml"]
