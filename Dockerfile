####################################################################################################
# base
####################################################################################################
FROM --platform=${BUILDPLATFORM:-linux/amd64} golang:1.22.4-alpine AS builder

RUN apk update && apk upgrade && \
    apk add --no-cache tzdata curl unzip

ARG TARGETOS=linux
ARG TARGETARCH=amd64
ARG COMMIT_SHA
ENV USER=perfmanuser
ENV UID=10001

# Create non-privileged user as processes running
# on containers run with root by default
RUN adduser \
    --disabled-password \
    --home "/home/${USER}" \
    --uid "${UID}" \
    "${USER}"

WORKDIR /app

# Cache deps before building and copying source
COPY go.mod .
COPY go.sum .
RUN go mod download
RUN go mod verify

COPY . .

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build  \
    -ldflags "-s -w -X github.com/numaproj-labs/numaflow-perfman/util.CommitSHA=$COMMIT_SHA"  \
    -v -o dist/perfman main.go

####################################################################################################
# perfman
####################################################################################################
FROM --platform=${BUILDPLATFORM:-linux/amd64} alpine:latest AS perfman

COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder /etc/passwd /etc/passwd
COPY --from=builder /etc/group /etc/group
COPY --from=builder /app/config /home/perfman/config
COPY --from=builder /app/dist/perfman /bin/perfman

USER perfmanuser:perfmanuser

WORKDIR /home/perfman

ENTRYPOINT ["/bin/ash"]
