# Build the RedCi Ops Panel as a static binary.
# Build context is the repo root:  docker build -f ops/Dockerfile -t redci-panel .
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY shared ./shared
COPY ops ./ops
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath \
      -ldflags "-s -w -X main.version=${VERSION}" \
      -o /out/panel ./ops/cmd/panel

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata \
 && adduser -D -u 1000 panel
COPY --from=build /out/panel /usr/local/bin/panel
RUN mkdir -p /etc/panel /var/lib/panel \
 && chown -R panel:panel /var/lib/panel
USER panel
ENV OPS_DB=/var/lib/panel/panel.db \
    OPS_LISTEN=0.0.0.0:8080
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/panel"]
CMD ["serve"]
