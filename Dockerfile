FROM alpine:3.11

RUN apk add --no-cache bash postgresql-client

WORKDIR /home/

COPY ./bin/joe ./bin/joe
COPY ./config ./config

EXPOSE $SERVER_PORT

CMD ["./bin/joe"]
