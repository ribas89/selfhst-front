# Build context MUST be ./icons (the PARENT of front/ and build/).
# This preserves upstream sources under build/build/ and frontend under front/.
FROM golang:1.26.3-alpine AS builder

WORKDIR /src/gallery
COPY front/go.mod front/main.go ./
COPY front/static/ ./static/
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/icons-front .

WORKDIR /src/icon-api
COPY build/build/go.mod build/build/main.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/icons-api .

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tini \
    && mkdir -p /app \
    && ln -s /icons /app/icons
COPY --from=builder /out/icons-front /usr/local/bin/icons-front
COPY --from=builder /out/icons-api /usr/local/bin/icons-api
COPY front/entrypoint.sh /usr/local/bin/start-icons
RUN chmod 0755 /usr/local/bin/start-icons

USER 1000:1000
ENV HOST=0.0.0.0 \
    PORT=80 \
    ICON_SOURCE=local \
    ICONS_DIR=/icons \
    ICONS_UPSTREAM=http://127.0.0.1:4050
EXPOSE 80

# Ensure both the gallery AND icon API work.
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD /usr/local/bin/icons-front -healthcheck && PORT=4050 /usr/local/bin/icons-api -healthcheck
ENTRYPOINT ["tini", "-g", "--", "/usr/local/bin/start-icons"]
