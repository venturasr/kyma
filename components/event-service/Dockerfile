FROM golang:1.9-alpine as builder

ARG DOCK_PKG_DIR=/go/src/github.com/kyma-project/kyma/components/event-service

RUN mkdir -p $DOCK_PKG_DIR

COPY ./ $DOCK_PKG_DIR
COPY ./licenses/ /app/licenses

WORKDIR $DOCK_PKG_DIR

RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o eventservice ./cmd/eventservice

FROM alpine
RUN apk update && apk add curl ngrep

LABEL source=git@github.com:kyma-project/kyma.git

COPY --from=builder /go/src/github.com/kyma-project/kyma/components/event-service .
COPY --from=builder /app/licenses /app/licenses

CMD ["/eventservice"]
