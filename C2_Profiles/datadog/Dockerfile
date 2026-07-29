FROM golang:1.26 AS builder

WORKDIR /Mythic/

COPY [".", "."]

RUN make build

FROM alpine

RUN apk add make

COPY --from=builder /main /main
COPY --from=builder /mythic_datadog_server /mythic_datadog_server

WORKDIR /Mythic/

COPY [".", "."]

CMD make run