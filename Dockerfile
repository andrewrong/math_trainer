FROM golang:1.24.11-bookworm AS build
ENV CGO_ENABLED=1
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o /out/math_trainer ./cmd/server

FROM debian:bookworm-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates poppler-utils \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY --from=build /out/math_trainer /app/math_trainer
COPY web /app/web
EXPOSE 8080
CMD ["/app/math_trainer", "-config", "/app/config.yaml", "-log", "/app/logs/server.log"]
