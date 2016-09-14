FROM alpine:3.4

ADD config/burrow.cfg /config/burrow.cfg

RUN apk update \
  && apk add bash \
  && apk add git bzr \
  && apk add go \
  && apk add ca-certificates \
  && apk add openssl \
  && wget https://raw.githubusercontent.com/pote/gpm/v1.4.0/bin/gpm && chmod +x gpm \
  && export GOPATH=/gopath \
  && export REPO_PATH="github.com/linkedin/Burrow" \
  && go get $REPO_PATH \
  && cd $GOPATH/src/${REPO_PATH} \
  && /gpm install \
  && go build \
  && mv Burrow /burrow-app \
  && apk del go git bzr \
  && rm -rf $GOPATH /var/cache/apk/* gpm

CMD [ "/launch-burrow.sh" ]