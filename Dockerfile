FROM alpine:3.16

RUN apk add --no-cache bash postgresql-client

WORKDIR /home/

COPY ./bin/joe ./bin/joe

EXPOSE $SERVER_PORT

CMD ["./bin/joe"]
