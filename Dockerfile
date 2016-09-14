FROM alpine:3.4

ADD config/burrow.cfg /config/burrow.cfg

RUN apk add --update bash \
  && apk --update add git bzr \
  && apk --update add go \
  && apk --update add alpine-sdk \
  && export GOPATH=/gopath \
  && git clone https://github.com/pote/gpm.git && cd gpm \
  && git checkout v1.4.0 \
  && ./configure \
  && make install \
  && export REPO_PATH="github.com/linkedin/Burrow" \
  && go get $REPO_PATH \
  && cd $GOPATH/src/${REPO_PATH} \
  && gpm install \
  && go build \
  && mv Burrow /burrow-app \
  && apk del alpine-sdk go gpm git bzr \
  && rm -rf $GOPATH /var/cache/apk/* /gpm

CMD [ "/launch-burrow.sh" ]