# syntax=docker/dockerfile:1

FROM golang:1.22 as builder

# Set destination for COPY
WORKDIR /go/src/github.com/rancher/cluster-api-provider-rancher

# Download Go modules
COPY go.mod go.sum ./
COPY pkg/apis ./pkg/apis
RUN go mod download

# Copy the source code. Note the slash at the end, as explained in
# https://docs.docker.com/reference/dockerfile/#copy
COPY *.go ./
COPY pkg ./pkg

RUN mkdir -p /assets && cd /assets \
    && curl -sfL https://github.com/rancher/system-agent/releases/download/v0.3.11/rancher-system-agent-amd64 -O \
    && curl -sfL https://github.com/rancher/system-agent/releases/download/v0.3.11/system-agent-uninstall.sh -O

# Build
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags "-extldflags -static" -o /cluster-api-provider-rancher

FROM registry.suse.com/bci/bci-base:15.6

COPY --from=builder /cluster-api-provider-rancher /
COPY --from=builder /assets /usr/share/rancher/ui/assets

ENTRYPOINT ["/cluster-api-provider-rancher"]