###
### Stage 0: builder
###
FROM golang:1.25.10-alpine AS builder
RUN apk update && apk upgrade && apk add build-base
WORKDIR /travel-token-matrix-app-service

COPY . .
RUN go build -o build/

###
### Stage 1: runtime
###
FROM alpine:3.21

RUN apk add libc6-compat

WORKDIR /travel-token-matrix-app-service
COPY --from=builder /travel-token-matrix-app-service/build .

ENTRYPOINT [ "./travel-token-matrix-app-service" ]