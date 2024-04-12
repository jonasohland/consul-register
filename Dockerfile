FROM golang:1.22 AS builder

ARG GIT_TAG

WORKDIR /build

COPY go.mod go.mod
COPY go.sum go.sum
COPY Makefile Makefile
COPY cmd cmd
COPY pkg pkg

# build
RUN make GOOS=linux GIT_TAG=${GIT_TAG} build

FROM scratch

COPY --from=builder /build/bin/consul-register /consul-register

EXPOSE 9645

ENTRYPOINT ["/consul-register"]
CMD [ "-l", ":9645" ]
