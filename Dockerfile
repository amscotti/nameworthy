# Keep the compiler version aligned with go.mod and mise.toml. The build is
# builder-agnostic (no BuildKit-only syntax): BuildKit fills TARGETOS and
# TARGETARCH for --platform builds, while other builders — Cloud Build's
# classic docker — leave them empty and Go builds for the native host.
ARG GO_VERSION=1.27.1
FROM golang:${GO_VERSION}-alpine AS build

RUN apk add --no-cache ca-certificates
WORKDIR /src
COPY . .

ARG TARGETOS
ARG TARGETARCH
ENV CGO_ENABLED=0 GOTOOLCHAIN=local
RUN GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -buildvcs=false -ldflags="-s -w" \
    -o /out/nameworthy ./cmd/nameworthy

# The runtime contains the static executable (including frontend assets),
# the trust bundle required by the TypeSafe HTTPS client, and its license.
FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/nameworthy /nameworthy
COPY LICENSE /LICENSE

LABEL org.opencontainers.image.title="nameworthy" \
      org.opencontainers.image.description="A WoW: Forever name auditor powered by TypeSafe Jev" \
      org.opencontainers.image.licenses="MIT"

USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/nameworthy"]
CMD ["-addr", ":8080"]
