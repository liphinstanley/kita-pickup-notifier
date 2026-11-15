# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM golang:1.27 AS build
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY go.mod ./
COPY *.go ./
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -tags timetzdata -ldflags="-s -w" -o /notifier .

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /notifier /notifier
ENTRYPOINT ["/notifier"]
