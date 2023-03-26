# syntax=docker/dockerfile:1.4

FROM golang:1.18-alpine AS build-dev
WORKDIR /go/src/app
COPY --link go.mod go.sum ./
RUN go mod download
COPY --link . .
RUN CGO_ENABLED=0 go install -buildvcs=false -trimpath -ldflags '-w -s'
FROM scratch
COPY --link --from=build-dev /go/bin/nostr-haikubot /go/bin/nostr-haikubot
CMD ["/go/bin/nostr-haikubot"]
