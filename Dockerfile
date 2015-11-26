FROM golang:onbuild
EXPOSE 8080

# no default values for these env:
#ENV SEQREDEPLOY_HOSTIP
#ENV TUTUM_USER
#ENV TUTUM_APIKEY
#ENV SEQREDEPLOY_SECRET

#FROM golang
#
#ADD . /go/src/github.com/dsteinkopf/seqredeploy
#
#RUN go get github.com/BurntSushi/toml && \
#	go get github.com/gorilla/websocket && \
#	go get github.com/tutumcloud/go-tutum/tutum && \
#	go install github.com/dsteinkopf/seqredeploy
#
#ENTRYPOINT /go/bin/seqredeploy
#
## Document that the service listens on port 8080.
#EXPOSE 8080