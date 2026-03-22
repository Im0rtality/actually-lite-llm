FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS builder

ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH GOARM=${TARGETVARIANT#v} \
    go build -trimpath -ldflags="-s -w" -o /actually-lite-llm .

FROM gcr.io/distroless/static-debian12

COPY --from=builder /actually-lite-llm /actually-lite-llm

EXPOSE 8080
ENTRYPOINT ["/actually-lite-llm"]
