FROM alpine:3.4

ADD *.go /kafka-lagcheck/

RUN apk update \
  && apk add bash \
  && apk add git bzr \
  && apk add go \
  && export GOPATH=/gopath \
  && export LAGCHECK_REPO_PATH="github.com/Financial-Times/kafka-lagcheck" \
  && mkdir -p $GOPATH/src/${LAGCHECK_REPO_PATH} \
  && cp -r /kafka-lagcheck/*.go $GOPATH/src/${LAGCHECK_REPO_PATH}/ \
  && cd $GOPATH/src/${LAGCHECK_REPO_PATH} \
  && go get -t ./... \
  && go build \
  && mv kafka-lagcheck /kafka-lagcheck-app \
  && rm -rf /kafka-lagcheck \
  && mv /kafka-lagcheck-app /kafka-lagcheck \
  && apk del go git bzr \
  && rm -rf $GOPATH /var/cache/apk/*

CMD [ "/kafka-lagcheck" ]