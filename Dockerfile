FROM golang:1.20-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go env -w GOPROXY=https://proxy.golang.org && go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /bin/server ./cmd/server

FROM gcr.io/distroless/static-debian11
COPY --from=build /bin/server /server
EXPOSE 50051
ENTRYPOINT ["/server"]