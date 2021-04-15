FROM golang:1.14.0-buster
WORKDIR /app
COPY . .
RUN go build -mod vendor -o service  .

FROM debian:10
WORKDIR /app
COPY --from=0 /app/service .

EXPOSE 8080
CMD ["/app/service"]