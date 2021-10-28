FROM golang AS build-env

# If this directory is changed here it must also be changed for the COPY
# statements at the end of this Dockerfile. Arg expansion in COPY statements
# seems to be broken
# https://stackoverflow.com/questions/43473236/docker-build-arg-and-copy#comment103817419_43473956.
ARG NOTICES=/THIRD_PARTY_NOTICES

ADD ./ /go/src/builder
WORKDIR /go/src/builder

# Build the builder tool.
RUN GO111MODULE=on CGO_ENABLED=0 go build -o /go/bin/main

# Pull the source for some additional packages so that their license files can
# be manually included in the image. Note that this license file capturing is
# not complete; the rest of the license files will be injected by our
# licensescan tool at a later point.
RUN go get golang.org/x/image honnef.co/go/tools github.com/go-gl/glfw github.com/chzyer/logex github.com/cncf/udpa/go github.com/cncf/xds/go
RUN mkdir -p $NOTICES/golang.org/x/image; cp /go/pkg/mod/golang.org/x/image*/font/gofont/ttfs/README $NOTICES/golang.org/x/image/
RUN mkdir -p $NOTICES/honnef.co/go/tools; cp /go/pkg/mod/honnef.co/go/tools*/LICENSE-THIRD-PARTY $NOTICES/honnef.co/go/tools/
RUN mkdir -p $NOTICES/github.com/go-gl/glfw; cp /go/pkg/mod/github.com/go-gl/glfw*/v3.2/glfw/glfw/deps/mingw/dinput.h $NOTICES/github.com/go-gl/glfw/
RUN mkdir -p $NOTICES/github.com/cncf/udpa/go; cp /go/pkg/mod/github.com/cncf/udpa/go*/LICENSE $NOTICES/github.com/cncf/udpa/go/
RUN mkdir -p $NOTICES/github.com/cncf/xds/go; cp /go/pkg/mod/github.com/cncf/xds/go*/LICENSE $NOTICES/github.com/cncf/xds/go/
# Fetch the LICENSE file for this dependency from github since it is not
# included in the code that `go get` fetches.
RUN mkdir -p $NOTICES/github.com/chzyer/logex; wget https://raw.githubusercontent.com/chzyer/logex/master/LICENSE -O $NOTICES/github.com/chzyer/logex/LICENSE

# Create a source tarball to distribute inside of the container image. Omit
# the mod/cache/ dir which just contains .mod files, not source code files.
# The size of the tarball is about 117 MB.
RUN tar -c /go/pkg/mod/* /go/src/builder -z -f /gke-windows-builder-source.tar.gz

FROM gcr.io/distroless/base-debian10
COPY --from=build-env /go/bin/main /bin/main
COPY --from=build-env /gke-windows-builder-source.tar.gz /
COPY --from=build-env /THIRD_PARTY_NOTICES/ /THIRD_PARTY_NOTICES/
ENTRYPOINT [ "/bin/main" ]
