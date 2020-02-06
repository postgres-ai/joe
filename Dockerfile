FROM alpine:3.11

WORKDIR /home/

COPY ./bin/joe ./bin/joe
COPY ./config ./config

EXPOSE $SERVER_PORT

CMD ["./bin/joe"]
