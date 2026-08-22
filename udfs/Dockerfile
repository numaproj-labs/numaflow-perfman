# Build the single UDF fixture binary (benchmark + validation modes). Context: repository root.
FROM golang:1.27 AS build
WORKDIR /src
COPY udfs/go.mod udfs/go.sum ./
RUN go mod download
COPY udfs/ .
RUN CGO_ENABLED=0 go build -o /out/udfs .

FROM gcr.io/distroless/static:nonroot
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/udfs /udfs
ENTRYPOINT ["/udfs"]
