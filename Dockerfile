# Сайт gate4.ai: один статический бинарник. Слушает только HTTP (127.0.0.1:8080,
# сеть хоста); TLS — Caddy за relay (deploy/compose.yaml, README).
FROM golang:1.27.1 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /gate4ai-www .

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /gate4ai-www /gate4ai-www
ENV GATE4AI_WWW_LISTEN=127.0.0.1:8080
ENTRYPOINT ["/gate4ai-www"]
