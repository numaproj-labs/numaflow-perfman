# Optional host CLI image — install the binary on the EC2 host for normal use.
FROM golang:1.27 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build \
    -ldflags="-X 'numa-perfman/internal/cli.Version=${VERSION}'" \
    -o /out/perfman ./cmd/perfman

FROM gcr.io/distroless/static:nonroot
COPY --from=build /out/perfman /perfman
ENTRYPOINT ["/perfman"]
