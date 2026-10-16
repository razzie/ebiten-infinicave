#!/bin/sh
GOOS=linux go build -trimpath ./cmd/infinicave
GOOS=windows go build -trimpath -ldflags="-H=windowsgui" ./cmd/infinicave
