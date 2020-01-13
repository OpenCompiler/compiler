FROM golang:latest
RUN apt-get update -y
RUN apt-get install -y musl-dev gcc
RUN mkdir -p $GOPATH/src/github.com/OpenCompiler/compiler
WORKDIR $GOPATH/src/github.com/OpenCompiler/compiler
ADD . ./
RUN go build --ldflags '-s -w -linkmode external -extldflags -static' -o /main


FROM scratch
ADD ./languages.yaml ./languages.yaml
COPY --from=0 /main /main
CMD ["/main"]
