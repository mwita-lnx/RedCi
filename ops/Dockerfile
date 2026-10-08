# Build the RedCi Ops Panel as a static binary.
# Build context is the repo root:  docker build -f ops/Dockerfile -t redci-panel .

# Stage 1: build the React SPA (Vite) → ops/web/app/dist, embedded by Go.
FROM node:22-alpine AS spa
WORKDIR /app
COPY ops/web/app/package.json ops/web/app/package-lock.json* ./
RUN npm install
COPY ops/web/app ./
RUN npm run build

# Stage 2: build the Go binary (embeds the SPA dist + generated templ).
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
RUN go install github.com/a-h/templ/cmd/templ@latest
COPY shared ./shared
COPY ops ./ops
COPY agent ./agent
# Bring in the built SPA so //go:embed all:dist finds real assets.
COPY --from=spa /app/dist ./ops/web/app/dist
RUN templ generate ./ops/web/components/
ARG VERSION=dev
# Build the Linux agent for amd64 + arm64 and place them where the panel embeds
# them (ops/web/agentbin/), so the panel can serve the install one-liner.
RUN mkdir -p ops/web/agentbin && \
    GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath \
      -ldflags "-s -w -X main.version=${VERSION}" \
      -o ops/web/agentbin/ops-agent-linux-amd64 ./agent/cmd/agent && \
    GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath \
      -ldflags "-s -w -X main.version=${VERSION}" \
      -o ops/web/agentbin/ops-agent-linux-arm64 ./agent/cmd/agent
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
