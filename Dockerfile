FROM golang:1.27.1-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /mezo .

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata && adduser -D -u 10001 mezo
USER mezo
COPY --from=build /mezo /usr/local/bin/mezo
EXPOSE 4173
ENTRYPOINT ["mezo"]
