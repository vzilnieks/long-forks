FROM golang:1.25-alpine AS builder
WORKDIR /src
COPY enodefromkey.go go.mod go.sum ./
RUN go mod download && CGO_ENABLED=0 go build -o /enodefromkey enodefromkey.go

FROM ethereum/client-go:v1.13.15
RUN sh -c 'command -v apk >/dev/null && apk add --no-cache bash jq openssl || (apt-get update && apt-get install -y --no-install-recommends bash jq && rm -rf /var/lib/apt/lists/*)'
WORKDIR /init
COPY --from=builder /enodefromkey /usr/local/bin/enodefromkey
COPY init.sh ./
COPY genesis.tmpl.json ./
ENTRYPOINT ["sh","/init/init.sh"]
