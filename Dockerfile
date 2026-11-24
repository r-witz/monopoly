FROM golang:1.27 AS build
WORKDIR /src
COPY . .
# The tests run first (rules and rooms) and a failure stops the build. The caches survive between
# builds: only what changed is compiled again.
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go test -short ./... && \
    CGO_ENABLED=0 go build -trimpath -tags nethttpomithttp2 -ldflags="-s -w" -o /quartiers .

# the binary carries everything, the client included: nothing else is needed
FROM scratch
COPY --from=build /quartiers /quartiers
USER 65534:65534
ENV PORT=3000
EXPOSE 3000
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --start-interval=1s CMD ["/quartiers", "healthcheck"]
ENTRYPOINT ["/quartiers"]
