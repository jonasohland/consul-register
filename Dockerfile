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

# compress
RUN apt update && \
    apt install -y curl xz-utils && \
    curl -sL -o upx-4.2.2-amd64_linux.tar.xz https://github.com/upx/upx/releases/download/v4.2.2/upx-4.2.2-amd64_linux.tar.xz && \
    tar xf upx-4.2.2-amd64_linux.tar.xz && \
    upx-4.2.2-amd64_linux/upx --lzma --best -vv --no-progress -o bin/consul-register-minified bin/consul-register  

FROM scratch

COPY --from=builder /build/bin/consul-register-minified /consul-register

EXPOSE 9645

ENTRYPOINT ["/consul-register"]
CMD [ "-l", ":9645" ]