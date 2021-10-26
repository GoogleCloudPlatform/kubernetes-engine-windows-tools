FROM golang AS build-env

ADD ./ /go/src/builder
WORKDIR /go/src/builder

# Build the builder tool.
RUN GO111MODULE=on CGO_ENABLED=0 go build -o /go/bin/main

# Create a source tarball to distribute inside of the container image. Omit
# the mod/cache/ dir which just contains .mod files, not source code files.
# The size of the tarball is just under 50 MB.
RUN rm -rf /go/pkg/mod/cache
RUN tar -c /go/pkg/mod/* /go/src/builder -z -f /gke-windows-builder-source.tar.gz

FROM gcr.io/distroless/base-debian10
COPY --from=build-env /go/bin/main /bin/main
COPY --from=build-env /gke-windows-builder-source.tar.gz /
ENTRYPOINT [ "/bin/main" ]
