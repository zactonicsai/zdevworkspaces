# wsctl — static binary image (handy for CI jobs that create/stop workspaces).
#   docker build -t workspace/wsctl:dev -f clients/wsctl/Dockerfile clients/wsctl
#   docker run --rm -e WSCTL_PORTAL=https://workspaces.example.com -e WSCTL_TOKEN=$TOKEN workspace/wsctl:dev list
FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY *.go ./
ARG VERSION=dev
RUN go vet ./... && go test ./... && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/wsctl .

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /out/wsctl /wsctl
USER 65532:65532
ENTRYPOINT ["/wsctl"]
