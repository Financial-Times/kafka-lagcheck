# kafka-lagcheck - Kafka Consumer Lag Checking

Just creates a healthcheck and good-to-go endpoint that's checking kafka consumer lags..

Connects to an application based on [github.com/linkedin/Burrow](https://github.com/linkedin/Burrow)

## Run locally

1. You need to set up a [burrow](https://github.com/Financial-Times/burrow) running locally.
2. `go get github.com/Financial-Times/kafka-lagcheck`
3. `cd $GOPATH/src/github.com/Financial-Times/kafka-lagcheck`
4. `go install`
5. `./kafka-lagcheck`

The go app is only serving as a forwarder, it makes requests to Burrow, and forms the result in an FT standard healthcheck format. e.g.

`curl localhost:8080/__health`
