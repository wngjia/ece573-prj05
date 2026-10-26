FROM golang:1.26-alpine AS build
WORKDIR /src
ENV GOEXPERIMENT=jsonv2
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /login-service ./login-service && \
    CGO_ENABLED=0 go build -o /marketplace-service ./marketplace-service

FROM alpine:3.21 AS login
COPY --from=build /login-service /login-service
EXPOSE 8081
ENTRYPOINT ["/login-service"]

FROM alpine:3.21 AS marketplace
COPY --from=build /marketplace-service /marketplace-service
EXPOSE 8080
ENTRYPOINT ["/marketplace-service"]
