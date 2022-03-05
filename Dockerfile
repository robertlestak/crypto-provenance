FROM golang:1.16 as builder

WORKDIR /app

COPY . .

RUN go build -o provenance . 

FROM golang:1.16 as app

WORKDIR /app

COPY --from=builder /app/provenance .

ENTRYPOINT [ "/app/provenance" ]
