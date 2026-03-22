FROM golang:1.22-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /actually-lite-llm .

FROM gcr.io/distroless/static-debian12

COPY --from=builder /actually-lite-llm /actually-lite-llm

EXPOSE 8080
ENTRYPOINT ["/actually-lite-llm"]
