# One image with every physis binary. The data (Gset graphs, UCI digits) is
# fetched by an init container at deploy time rather than redistributed.
FROM golang:1.26-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/ ./cmd/physis-scheduler ./cmd/physis-agent ./cmd/physis-conformance ./cmd/physis-load ./cmd/physis-data ./cmd/physis-mcp

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/ /usr/local/bin/
USER nonroot
ENTRYPOINT ["/usr/local/bin/physis-scheduler"]
