# kafka-lagcheck - Kafka Consumer Lag Checking

[![CircleCI](https://circleci.com/gh/Financial-Times/kafka-lagcheck.svg?style=shield)](https://circleci.com/gh/Financial-Times/kafka-lagcheck) [![Coverage Status](https://coveralls.io/repos/github/Financial-Times/kafka-lagcheck/badge.svg)](https://coveralls.io/github/Financial-Times/kafka-lagcheck)

Just creates a healthcheck and good-to-go endpoint that's checking kafka consumer lags.
Connects to an application based on [github.com/linkedin/Burrow](https://github.com/linkedin/Burrow)
The go app is only serving as a forwarder, it makes requests to Burrow, and forms the result in an FT standard healthcheck format. e.g.

## Installation

1. You need to set up a [burrow](https://github.com/Financial-Times/burrow) running locally.
2. `go get github.com/Financial-Times/kafka-lagcheck`
3. `cd $GOPATH/src/github.com/Financial-Times/kafka-lagcheck`
4. `go get -u github.com/kardianos/govendor `
5. `$GOPATH/bin/govendor sync `
6. `go install`

## Run locally
 To run the app, first install it, then run the following command:
 `./kafka-lagcheck`

## Service endpoints
### Health endpoint:

For each consumer, check if it lags behind.
- Using curl: `curl localhost:8080/__health`

### GTG endpoint
- Using curl: `curl localhost:8080/__gtg`

## Other information
### Whitelisting environments
To filter out the list of consumers that are checked for lag, a whitelist of environments can be specified, consequently
the app will monitor only consumers from the current cluster and Kafka Bridges that are located in environments that belong
to the whitelist.

The environments whitelist should be stored in the environment variable with name `WHITELISTED_ENVS`
As an example, if the kafka-lagcheck from `pub-prod-env1` environment has WHITELISTED_ENVS = `prod-env1, prod-env2`, then only consumers from `pub-prod-env1` and kafka bridges from `prod-env1` and `prod-env2` will appear
in the healthchecks list, while kafka-bridges from other environments (e.g. `pre-prod`) will be ignored.
